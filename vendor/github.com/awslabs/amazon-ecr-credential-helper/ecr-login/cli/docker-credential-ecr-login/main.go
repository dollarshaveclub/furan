// Copyright 2016 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//	http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package main

import (
	"flag"
	"fmt"
	"os"

	ecr "github.com/awslabs/amazon-ecr-credential-helper/ecr-login"
	"github.com/awslabs/amazon-ecr-credential-helper/ecr-login/api"
	"github.com/awslabs/amazon-ecr-credential-helper/ecr-login/config"
	"github.com/awslabs/amazon-ecr-credential-helper/ecr-login/version"
	log "github.com/cihub/seelog"
	"github.com/docker/docker-credential-helpers/credentials"
)

const banner = `amazon-ecr-credential-helper
Version:    %s
Git commit: %s
`

func main() {
	var versionFlag bool
	flag.BoolVar(&versionFlag, "v", false, "print version and exit")
	flag.Parse()

	// Exit safely when version is used
	if versionFlag {
		fmt.Printf(banner, version.Version, version.GitCommitSHA)
		os.Exit(0)
	}

	defer log.Flush()
	config.SetupLogger()
	credentials.Serve(ecr.ECRHelper{ClientFactory: api.DefaultClientFactory{}})
}
