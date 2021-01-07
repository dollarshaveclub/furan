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
sed -e "s/{VALUE}/$(echo -n ${GITHUB_TOKEN}|base64)/g" < ./github-token.yaml > ./out/k8s-github-token.yaml
sed -e "s/{VALUE}/$(echo -n ${QUAY_TOKEN}|base64)/g" < ./quay-token.yaml > ./out/k8s-quay-token.yaml
sed -e "s#{AKIDVALUE}#$(echo -n ${AWS_AKID}|base64)#g; s#{SAKVALUE}#$(echo -n ${AWS_SAK}|base64)#g" < ./aws-access-key.yaml > ./out/k8s-aws-access-key.yaml
sed -e "s#{URIVALUE}#$(echo -n ${DB_URI}|base64)#g; s#{CEKVALUE}#$(echo -n ${DB_CEK}|base64 -w0)#g" < ./db.yaml > ./out/k8s-db.yaml

kubectl create -f ./out/
