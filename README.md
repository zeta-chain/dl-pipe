`dl-pipe` allows you to safely and easily resumable download files in pipelines. Resumable downloads via commands (like `wget`, `curl`, etc) typically require the downloaded content to be written out to disk. With this tool, you can do:


```
dl-pipe https://example.invalid/my-file.tar | tar x
```

We also provide an expected hash via the `-hash` option to ensure that the download content is correct. Make sure you set `set -eo pipefail` to ensure your script stops on errors.

Install with `go install github.com/zeta-chain/dl-pipe@latest`.