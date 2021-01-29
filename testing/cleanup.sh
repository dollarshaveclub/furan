#!/bin/bash

helm delete furan2-integration
helm delete furan2
kubectl get jobs -o custom-columns=:metadata.name |grep furan-build |xargs kubectl delete job

