`dl-pipe` allows you to safely and easily resumable download files in pipelines. Resumable downloads via commands (like `wget`, `curl`, etc) typically require the downloaded content to be written out to disk. With this tool, you can do:


```
dl-pipe https://example.invalid/my-file.tar | tar x
```

You may also provide the parts of a multipart tar file and it will be reassembled.

```
dl-pipe https://example.invalid/my-file.tar.part1 https://example.invalid/my-file.tar.part2 https://example.invalid/my-file.tar.part3 | tar x
```

We use this to workaround the 5TB size limit of most object storage providers.

We also provide an expected hash via the `-hash` option to ensure that the download content is correct. Make sure you set `set -eo pipefail` to ensure your script stops on errors.

Install with `go install github.com/zeta-chain/dl-pipe@latest`.