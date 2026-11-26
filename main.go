// Terraform provider for SonarQube.
package main

import (
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6/tf6server"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider"
)

// version is set with the -ldflags option at release time. It stays "dev" for
// local builds.
var version = "dev"

func main() {
	var debug bool

	flag.BoolVar(&debug, "debug", false, "set to true to run the provider with support for debuggers like delve")
	flag.Parse()

	var opts []tf6server.ServeOpt
	if debug {
		opts = append(opts, tf6server.WithManagedDebug())
	}

	// The address must agree with the source address in the Terraform
	// configuration and with the dev_overrides key for local development.
	if err := tf6server.Serve("registry.terraform.io/sonarsource/sonarqube", serverFactory(), opts...); err != nil {
		log.Fatal(err.Error())
	}
}

// serverFactory returns the provider server that main serves. Its requests
// name the operation in the optional telemetry.
func serverFactory() func() tfprotov6.ProviderServer {
	return provider.NewProtocol6(version)
}
