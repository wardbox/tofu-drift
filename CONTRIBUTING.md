# Contributing

Open a pull request against `main`. `go build ./... && go vet ./... && go test ./...` and `golangci-lint run` must pass.

## Sign your commits (DCO)

Every commit must carry a `Signed-off-by` line matching its author, certifying the [Developer Certificate of Origin](https://developercertificate.org/): you wrote the change or otherwise have the right to submit it under this project's license. `git commit -s` adds the line. To sign off an existing branch:

```sh
git rebase --signoff origin/main
git push --force-with-lease
```

The `dco` check fails a pull request with any commit not signed off by its author.

## Adding a scanner

A new scanner in `newScanners` (`cmd/tofu-drift/scan.go`) needs an entry in `coveredTypes` (`cmd/tofu-drift/readme_test.go`) and a row in the README Coverage table. `TestReadmeCoverage` fails until both exist.
