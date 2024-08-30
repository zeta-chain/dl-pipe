package dlpipe

import (
	"bytes"
	"context"
	"fmt"
	"hash"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/miolini/datacounter"
)

const (
	acceptRangeHeader  = "Accept-Ranges"
	rangeHeader        = "Range"
	contentRangeHeader = "Content-Range"
	oneMB              = 1 << 20 // 1 MB in bytes
)

type DownloadOpt func(*downloader)

func WithHTTPClient(client *http.Client) DownloadOpt {
	return func(d *downloader) {
		d.httpClient = client
	}
}

func WithHasher(hasher hash.Hash) DownloadOpt {
	return func(d *downloader) {
		d.tmpWriter = io.MultiWriter(d.tmpWriter, hasher)
	}
}

// WithExpectedHash hashes the download content and asserts it matches the given hash.
func WithExpectedHash(hasher hash.Hash, expected []byte) DownloadOpt {
	return func(d *downloader) {
		d.tmpWriter = io.MultiWriter(d.tmpWriter, hasher)
		d.finalFuncs = append(d.finalFuncs, func() error {
			givenHash := hasher.Sum(nil)
			if !bytes.Equal(givenHash, expected) {
				return ErrHashMismatch{
					ExpectedHash: expected,
					GivenHash:    givenHash,
				}
			}
			return nil
		})
	}
}

func WithHeaders(headers map[string]string) DownloadOpt {
	return func(d *downloader) {
		d.headers = headers
	}
}

type ProgressFunc func(currentLength uint64, totalLength uint64)

func WithProgressFunc(progressFunc ProgressFunc, interval time.Duration) DownloadOpt {
	return func(d *downloader) {
		d.progressFunc = progressFunc
		d.progressInterval = interval
	}
}

func WithRetryParameters(params RetryParameters) DownloadOpt {
	return func(d *downloader) {
		d.retryParameters = params
	}
}

type RetryParameters struct {
	MaxRetries     int
	BaseWait       time.Duration
	WaitMultiplier int

	currentWait time.Duration
	retryCtr    int
	prevPos     uint64
}

func (p *RetryParameters) Wait(ctx context.Context, currentPosition uint64) error {
	if p.retryCtr >= p.MaxRetries {
		return ErrRetryParametersExceeded
	}
	// reset wait period if first read or we are making sufficient progress
	if p.currentWait == 0 || (p.prevPos != 0 && currentPosition > p.prevPos+oneMB) {
		p.currentWait = p.BaseWait
	}
	p.prevPos = currentPosition
	defer func() {
		p.retryCtr++
		p.currentWait *= time.Duration(p.WaitMultiplier)
	}()

	select {
	case <-time.After(p.currentWait):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func DefaultRetryParameters() RetryParameters {
	return RetryParameters{
		MaxRetries:     5,
		BaseWait:       250 * time.Millisecond,
		WaitMultiplier: 2,
	}
}

type downloader struct {
	// these fields are set once
	url             string
	writer          *datacounter.WriterCounter
	httpClient      *http.Client
	retryParameters RetryParameters

	// these fields are used by option functions
	tmpWriter        io.Writer
	finalFuncs       []func() error
	headers          map[string]string
	progressFunc     ProgressFunc
	progressInterval time.Duration

	// these fields are updated at runtime
	contentLength int64
}

func (d *downloader) progressReportLoop(ctx context.Context) {
	t := time.NewTicker(d.progressInterval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			d.progressFunc(d.writer.Count(), uint64(d.contentLength))
		case <-ctx.Done():
			return
		}
	}
}

func (d *downloader) runInner(ctx context.Context) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.url, nil)
	if err != nil {
		return nil, NonRetryableWrapf("create request: %w", err)
	}

	for hKey, hValue := range d.headers {
		req.Header.Set(hKey, hValue)
	}

	downloadFrom := d.writer.Count()
	if downloadFrom > 0 {
		req.Header.Set(rangeHeader, fmt.Sprintf("bytes=%d-", downloadFrom))
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	if resp.StatusCode == http.StatusBadGateway {
		return nil, fmt.Errorf("bad gateway")
	}
	if d.contentLength == 0 {
		d.contentLength = resp.ContentLength

		if resp.StatusCode != http.StatusOK {
			return nil, NonRetryableWrapf("unexpected status code on first read: %d", resp.StatusCode)
		}
		return resp.Body, nil
	}

	if resp.StatusCode != http.StatusPartialContent {
		return nil, NonRetryableWrapf("unexpected status code on subsequent read: %d", resp.StatusCode)
	}

	// Validate we are receiving the right portion of partial content
	var respStart, respEnd, respTotal int64
	_, err = fmt.Sscanf(
		strings.ToLower(resp.Header.Get(contentRangeHeader)),
		"bytes %d-%d/%d",
		&respStart, &respEnd, &respTotal,
	)
	if err != nil {
		_ = resp.Body.Close()
		return nil, NonRetryableWrapf("error parsing response content-range header: %w", err)
	}

	if uint64(respStart) != downloadFrom {
		_ = resp.Body.Close()
		return nil, NonRetryableWrapf("unexpected response range start (expected %d, got %d)", downloadFrom, respStart)
	}
	if respEnd != d.contentLength-1 {
		_ = resp.Body.Close()
		return nil, NonRetryableWrapf("unexpected response range end (expected %d, got %d)", d.contentLength-1, respEnd)
	}
	if respTotal != d.contentLength {
		_ = resp.Body.Close()
		return nil, NonRetryableWrapf("unexpected response range total (expected %d, got %d)", d.contentLength-1, respTotal)
	}
	return resp.Body, nil
}

func (d *downloader) run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if d.progressFunc != nil {
		go d.progressReportLoop(ctx)
	}
	for {
		body, err := d.runInner(ctx)
		if err != nil {
			return err
		}
		defer body.Close()
		_, err = io.Copy(d.writer, body)
		if err == nil {
			break
		}
		err = d.retryParameters.Wait(ctx, d.writer.Count())
		if err != nil {
			return err
		}
	}

	for _, finalFunc := range d.finalFuncs {
		if err := finalFunc(); err != nil {
			return err
		}
	}
	return nil
}

func DownloadURL(ctx context.Context, url string, writer io.Writer, opts ...DownloadOpt) error {
	d := &downloader{
		url:       url,
		tmpWriter: writer,
		httpClient: &http.Client{
			Transport: &http.Transport{
				IdleConnTimeout:       10 * time.Second,
				ResponseHeaderTimeout: 10 * time.Second,
			},
		},
		retryParameters: DefaultRetryParameters(),
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(d)
	}
	d.writer = datacounter.NewWriterCounter(d.tmpWriter)
	return d.run(ctx)
}
