package dlpipe

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/test-go/testify/assert"
	"github.com/test-go/testify/require"
)

const (
	fileSize    = 5 * 1337 * 1337
	interruptAt = 1999999
)

func TestUninterruptedDownload(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	serverURLs, expectedHash, cleanup := serveInterruptedTestFiles(t, fileSize, 0, 1)
	defer cleanup()

	hasher := sha256.New()

	err := DownloadURLMultipart(ctx, serverURLs, io.Discard, WithExpectedHash(hasher, expectedHash))
	r.NoError(err)

	givenHash := hasher.Sum(nil)
	r.Equal(expectedHash, givenHash)
}

func TestUninterruptedMismatch(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	serverURLs, _, cleanup := serveInterruptedTestFiles(t, fileSize, 0, 1)
	defer cleanup()

	hasher := sha256.New()

	err := DownloadURLMultipart(ctx, serverURLs, io.Discard, WithExpectedHash(hasher, []byte{}))
	r.Error(err)
}

func TestInterruptedDownload(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	serverURLs, expectedHash, cleanup := serveInterruptedTestFiles(t, fileSize, interruptAt, 1)
	defer cleanup()

	hasher := sha256.New()

	err := DownloadURLMultipart(ctx, serverURLs, io.Discard, WithExpectedHash(hasher, expectedHash))
	r.NoError(err)
}

func TestDownloadMultipart(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	serverURLs, expectedHash, cleanup := serveInterruptedTestFiles(t, fileSize, 0, 10)
	defer cleanup()

	hasher := sha256.New()

	err := DownloadURLMultipart(ctx, serverURLs, io.Discard, WithExpectedHash(hasher, expectedHash))
	r.NoError(err)
}

func TestDownloadMultipartInterrupted(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	serverURLs, expectedHash, cleanup := serveInterruptedTestFiles(t, fileSize, interruptAt, 10)
	defer cleanup()

	hasher := sha256.New()

	err := DownloadURLMultipart(ctx, serverURLs, io.Discard, WithExpectedHash(hasher, expectedHash))
	r.NoError(err)
}

func TestErrNonRetryable(t *testing.T) {
	err := NonRetryableWrapf("test")
	require.True(t, errors.Is(err, ErrNonRetryable{}))
}

// derrived from https://github.com/vansante/go-dl-stream/blob/e29aef86498f37d3506126bc258193f1c913ea55/download_test.go#L166
func serveInterruptedTestFiles(t *testing.T, fileSize, interruptAt int64, parts int) ([]string, []byte, func()) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	hasher := sha256.New()
	filePaths := []string{}
	urls := []string{}

	for i := 0; i < parts; i++ {
		rndFile, err := os.CreateTemp(os.TempDir(), "random_file_*.rnd")
		assert.NoError(t, err)
		filePath := rndFile.Name()
		filePaths = append(filePaths, filePath)
		filePathBase := filepath.Base(filePath)

		_, err = io.Copy(io.MultiWriter(hasher, rndFile), io.LimitReader(rand.Reader, fileSize))
		assert.NoError(t, err)
		assert.NoError(t, rndFile.Close())

		mux.HandleFunc(filePath, func(writer http.ResponseWriter, request *http.Request) {
			log.Printf("Serving random interrupted file %s (size: %d, interuptAt: %d), Range: %s", filePathBase, fileSize, interruptAt, request.Header.Get(rangeHeader))

			http.ServeFile(&interruptibleHTTPWriter{
				ResponseWriter: writer,
				writer:         writer,
				interruptAt:    interruptAt,
			}, request, filePath)

		})
		urls = append(urls, server.URL+filePath)

	}

	return urls, hasher.Sum(nil), func() {
		for _, path := range filePaths {
			_ = os.Remove(path)
		}
	}
}

type interruptibleHTTPWriter struct {
	http.ResponseWriter

	writer      io.Writer
	written     int64
	interruptAt int64
	mu          sync.Mutex
}

// Write interrupts after writing a certain size
func (w *interruptibleHTTPWriter) Write(buf []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.written += int64(len(buf))
	if w.interruptAt > 0 && w.written > w.interruptAt {
		offset := len(buf) - int(w.written-w.interruptAt)
		n, err = w.writer.Write(buf[:offset])
		if err != nil {
			log.Printf("Error writing response: %v", err)
			return n, err
		}
		log.Printf("Interrupting download at %d bytes", w.interruptAt)
		return n, fmt.Errorf("interrupt size (%d bytes) reached", w.interruptAt)
	}
	return w.writer.Write(buf)
}
