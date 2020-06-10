check:
	./check.sh

proto:
	cd protos && protoc --go_out=plugins=grpc,paths=source_relative:../pkg/generated/furanrpc api.proto