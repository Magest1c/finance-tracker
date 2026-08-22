$ErrorActionPreference = "Stop"

$unformatted = gofmt -l ./cmd ./tests
if ($unformatted) {
    throw "Go files require formatting: $unformatted"
}

go vet ./...
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

if ((go env CGO_ENABLED).Trim() -eq "1") {
    go test -race -coverprofile coverageprofile -covermode atomic ./cmd/api
} else {
    Write-Warning "CGO is disabled; running coverage without the race detector. CI runs the race detector on Linux."
    go test -coverprofile coverageprofile -covermode atomic ./cmd/api
}
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

go tool cover -func coverageprofile | Select-Object -Last 1

go test -v ./tests/api
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
