package main

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"flag"
	"fmt"
	"hash"
	"os"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	dlpipe "github.com/zeta-chain/dl-pipe"
)

// Custom type to store multiple header values
type headerFlag []string

// Method to satisfy the flag.Value interface
func (h *headerFlag) String() string {
	return strings.Join(*h, ", ")
}

// Method to add a new header from the command line
func (h *headerFlag) Set(value string) error {
	*h = append(*h, value)
	return nil
}

func getHashOpt(hashArg string) dlpipe.DownloadOpt {
	if hashArg == "" {
		return nil
	}

	parts := strings.SplitN(hashArg, ":", 2)
	if len(parts) != 2 {
		fmt.Fprintf(os.Stderr, "Invalid hash: %s\n", hashArg)
		os.Exit(1)
	}

	var hasher hash.Hash
	switch parts[0] {
	case "sha1":
		hasher = sha1.New()
	case "sha256":
		hasher = sha256.New()
	case "sha512":
		hasher = sha512.New()
	case "md5":
		hasher = md5.New()
	default:
		fmt.Fprintf(os.Stderr, "Unsupported hash algorithm: %s\n", parts[0])
		os.Exit(1)
	}

	hashBytes, err := hex.DecodeString(parts[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid hash: %s\n", hashArg)
		os.Exit(1)
	}

	return dlpipe.WithExpectedHash(hasher, hashBytes)
}

const progressFuncInterval = time.Second * 10

func getProgressFunc() dlpipe.ProgressFunc {
	prevLength := uint64(0)
	return func(currentLength uint64, totalLength uint64) {
		currentLengthStr := humanize.Bytes(currentLength)
		totalLengthStr := humanize.Bytes(totalLength)

		rate := float64(currentLength-prevLength) / progressFuncInterval.Seconds()
		rateStr := humanize.Bytes(uint64(rate))
		prevLength = currentLength

		percent := float64(currentLength) / float64(totalLength) * 100

		fmt.Fprintf(os.Stderr, "Downloaded %s of %s (%.1f%%) at %s/s\n", currentLengthStr, totalLengthStr, percent, rateStr)
	}
}

func getProgressOpt(progress bool) dlpipe.DownloadOpt {
	if !progress {
		return nil
	}
	return dlpipe.WithProgressFunc(getProgressFunc(), progressFuncInterval)
}

func main() {
	var headers headerFlag
	var hash string
	var progress bool
	flag.Var(&headers, "header", "Header to include in the HTTP request. Can be specified multiple times.")
	flag.StringVar(&hash, "hash", "", "Expected hash of the downloaded content with algorithm prefix (sha1,sha256,sha512,md5)")
	flag.BoolVar(&progress, "progress", false, "Show download progress")
	flag.Parse()

	url := flag.Arg(0)
	if url == "" {
		fmt.Fprintf(os.Stderr, ("URL is required"))
		os.Exit(1)
	}

	ctx := context.Background()

	headerMap := make(map[string]string)
	for _, header := range headers {
		parts := strings.SplitN(header, ":", 2)
		if len(parts) != 2 {
			fmt.Fprintf(os.Stderr, "Invalid header: %s\n", header)
			os.Exit(1)
		}
		headerMap[parts[0]] = parts[1]
	}

	err := dlpipe.DownloadURL(
		ctx,
		url,
		os.Stdout,
		dlpipe.WithHeaders(headerMap),
		getHashOpt(hash),
		getProgressOpt(progress),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
