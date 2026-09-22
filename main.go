// Terraform provider for SonarQube.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider"
)

// version is set with the -ldflags option at release time. It stays "dev" for
// local builds.
var version = "dev"

func main() {
	var debug bool

	flag.BoolVar(&debug, "debug", false, "set to true to run the provider with support for debuggers like delve")
	flag.Parse()

	opts := providerserver.ServeOpts{
		// The address must agree with the source address in the Terraform
		// configuration and with the dev_overrides key for local development.
		Address: "registry.terraform.io/sonarsource/sonarqube",
		Debug:   debug,
	}

	if err := providerserver.Serve(context.Background(), provider.New(version), opts); err != nil {
		log.Fatal(err.Error())
	}
}
