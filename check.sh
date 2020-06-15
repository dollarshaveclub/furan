set -e -x

go build
go vet $(go list ./... |grep pkg/)
dep check
go test -cover $(go list ./... |grep pkg/)
docker build -t at .