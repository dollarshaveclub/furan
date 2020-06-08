#!/bin/sh

mockgen -package mocks github.com/dollarshaveclub/furan/lib/datalayer DataLayer > ./mock_datalayer.go
mockgen -package mocks github.com/dollarshaveclub/furan/lib/kafka EventBusProducer  > ./mock_eventbusproducer.go
mockgen -package mocks github.com/dollarshaveclub/furan/lib/github_fetch CodeFetcher > ./mock_codefetcher.go
mockgen -package mocks github.com/dollarshaveclub/furan/lib/builder ImageBuildClient > ./mock_image_build_client.go
mockgen -package mocks github.com/dollarshaveclub/furan/lib/metrics MetricsCollector > ./mock_metrics_collector.go
mockgen -package mocks github.com/dollarshaveclub/furan/lib/s3 ObjectStorageManager > ./mock_object_storage_manager.go
mockgen -package mocks github.com/dollarshaveclub/furan/lib/squasher ImageSquasher > ./mock_image_squasher.go
mockgen -package mocks github.com/dollarshaveclub/furan/lib/tagcheck ImageTagChecker > ./mock_image_tag_checker.go
mockgen -package mocks github.com/dollarshaveclub/furan/lib/consul ConsulKV  > ./mock_consul_kv.go

# mockgen improperly inserts vendor directory into imports so we have to manually filter out
# https://github.com/golang/mock/issues/30
if [[ -f $(which gsed) ]]; then
	SED="gsed"
else
	SED="sed"
fi
$SED -i 's/github.com\/dollarshaveclub\/furan\/vendor\///g' mock_*.go
