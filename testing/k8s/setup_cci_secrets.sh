#!/bin/bash

# sets up kubernetes secrets from CircleCI secret env vars

if [[ -z "${GITHUB_TOKEN}" ]]; then
  echo "github token missing from env"
  exit 1
fi

if [[ -z "${QUAY_TOKEN}" ]]; then
  echo "quay token missing from env"
  exit 1
fi

if [[ -z "${AWS_AKID}" ]]; then
  echo "aws access key id missing from env"
  exit 1
fi

if [[ -z "${AWS_SAK}" ]]; then
  echo "aws access key id missing from env"
  exit 1
fi

if [[ -z "${DB_URI}" ]]; then
  echo "db uri missing from env"
  exit 1
fi

if [[ -z "${DB_CEK}" ]]; then
  echo "db enc key missing from env"
  exit 1
fi

mkdir -p out

# MacOS vs Linux
if [[ "$(uname)" == "Darwin" ]]; then
	B64=( "base64" )
else
	B64=( "base64" "-w0" )
fi

set -e

sed -e "s/{VALUE}/$(echo -n ${GITHUB_TOKEN}|"${B64[@]}")/g" < ./github-token.yaml > ./out/k8s-github-token.yaml
sed -e "s/{VALUE}/$(echo -n ${QUAY_TOKEN}|"${B64[@]}")/g" < ./quay-token.yaml > ./out/k8s-quay-token.yaml
sed -e "s#{AKIDVALUE}#$(echo -n ${AWS_AKID}|"${B64[@]}")#g; s#{SAKVALUE}#$(echo -n ${AWS_SAK}|"${B64[@]}")#g" < ./aws-access-key.yaml > ./out/k8s-aws-access-key.yaml
sed -e "s#{URIVALUE}#$(echo -n ${DB_URI}|"${B64[@]}")#g; s#{CEKVALUE}#$(echo -n ${DB_CEK}|"${B64[@]}")#g" < ./db.yaml > ./out/k8s-db.yaml

kubectl create -f ./out/
