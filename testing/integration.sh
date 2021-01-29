#!/bin/bash

# This is for running integration tests locally against the active k8s cluster
# This parallels the CircleCI config

# run in the repository root
if [[ ! -d "./.git" ]]; then
  echo "run in repository root directory"
  exit 1
fi

# make sure kube context is minikube
if [[ "$(kubectl config current-context)" != "minikube" ]]; then
  echo "kubectl context must be minikube (edit script if you're sure you know what you're doing)"
  exit 1
fi

if ! kubectl get clusterrolebinding default-cluster-admin >/dev/null 2>&1; then
  echo "setting up rbac"
  kubectl create clusterrolebinding default-cluster-admin --clusterrole cluster-admin --serviceaccount=default:default
fi

# make sure you have required secrets in the shell environment
if ! kubectl get secret/aws-access-key >/dev/null 2>&1; then
  echo "setting up secrets"
  pushd ./testing/k8s || exit 1
  ./setup_cci_secrets.sh || exit 1
  popd || exit 1
fi

# database
if ! helm list | grep -q postgres; then
  echo "installing postgres"
  helm install postgres stable/postgresql --set 'image.tag=12,postgresqlPassword=root,postgresqlDatabase=furan,persistence.enabled=false,fullnameOverride=postgresql' || exit 1
fi

# furan server
if ! helm list | grep -q furan2; then
  echo "installing furan server"
  helm install furan2 .helm/charts/furan2 --set 'run_migrations=true,app.verbose=true,app.tls.use_dev_cert=true,app.secrets_backend=env,image.repository=furan2,image.tag=integration,is_dqa=true,serviceAccountName=default,app.builder_image=furan2-builder:integration' || exit 1
fi

# integration tests
if helm list | grep -q furan2-integration; then
  helm delete furan2-integration
fi

helm install furan2-integration .helm/charts/furan2-integration --wait=false || exit 1

jobname="furan2-integration"
until [[ $SECONDS -gt 600 ]] ||
  [[ $(kubectl get jobs ${jobname} -o jsonpath='{.status.conditions[?(@.type=="Failed")].status}') == "True" ]] ||
  [[ $(kubectl get jobs ${jobname} -o jsonpath='{.status.conditions[?(@.type=="Complete")].status}') == "True" ]]; do
  echo "waiting for job completion..."
  sleep 5
done
success=$(kubectl get jobs ${jobname} -o jsonpath='{.status.conditions[?(@.type=="Complete")].status}')
if [[ "${success}" == "True" ]]; then
  echo "job success"
  helm delete furan2-integration
  exit 0
else
  echo "job failed or timeout"
  kubectl get pods
  kubectl describe pods
  kubectl describe job/${jobname}
  kubectl logs job/${jobname}
  exit 1
fi

