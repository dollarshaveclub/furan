<p align="center">
<img with="304" height="300" src="https://s3-us-west-2.amazonaws.com/s.cdpn.io/12437/furan_icon.svg" alt="Furan" />
</p>
<h1 align="center">Furan</h1>

-----

<h4 align="center">Scale out Docker builds</h4>

-----

[![Go Report Card](https://goreportcard.com/badge/github.com/dollarshaveclub/furan)](https://goreportcard.com/report/github.com/dollarshaveclub/furan)

Furan is a horizontally-scalable Docker build microservice (API) that builds and pushes Docker images from a specified GitHub repository to a specified target (registry or S3).

<h3 align="center">&middot;&middot;&middot;</h3>

## What is Furan's advantage?

-  **Furan is fast!** Optimized for build speed, Furan runs operations in memory instead of disk. Optionally, it can be configured to run all builds within a RAM disk. Furan streams directly from GitHub to a local Docker daemon without temporary files.

-  **Furan is stateless!** Furan is deployed as an essentially stateless API application, allowing it to be scaled out. Furan does not shell out to execute docker commands. It leverages the Docker Engine API.

-  **Furan is hookable!** Furan is triggered on demand via GRPC or HTTPS. Builds can be triggered on one node and monitored on any other (allows round-robin load balancing).

-  **Furan supports Docker pushs and S3 Deploys!** Furan supports pushing to Docker registries (public or private) as well as pre-squashing and deploying tarballs directly to S3.

- **Furan is secure!** Furan integrates with [Vault](https://www.vaultproject.io) for secure storage of service credentials (Docker registries, AWS). Furan supports token and AppID authentication.

- **Furan is instrumented!** Furan has full Datadog integration so you can monitor the size of your docker images over time, build durations, build failures and more. [Screenshot](https://s3.amazonaws.com/dsc-misc/furan-datadog-dashboard.png)

## API

The native API for Furan is based on [GRPC](http://www.grpc.io) and supports
a number of RPC calls. See the [protobuf definition](https://github.com/dollarshaveclub/furan/blob/master/protos/models.proto#L5-L10)
for details.

An [HTTPS adapter](https://github.com/dollarshaveclub/furan/blob/master/HTTP-API.md) is
available for testing convenience.

## Docs
-  [Quick Start](https://github.com/dollarshaveclub/furan/blob/master/docs/QUICKSTART.md)

## CLI

See the help output for full details: ``furan --help``

