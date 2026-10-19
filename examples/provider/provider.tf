terraform {
  required_version = ">= 1.6.1"

  required_providers {
    sonarqube = {
      source  = "sonarsource/sonarqube"
      version = "0.1.0-alpha.1"
    }
  }
}

# The token can also come from the SONARQUBE_TOKEN environment variable.
provider "sonarqube" {
  token = var.sonarqube_token
}

variable "sonarqube_token" {
  type      = string
  sensitive = true
}
