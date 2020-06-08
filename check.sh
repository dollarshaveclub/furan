set -e -x

export GO111MODULE=off

go build
go vet $(go list ./... |grep pkg/)
dep check
go test -cover $(go list ./... |grep pkg/)
docker build -t at .