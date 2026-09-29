# A binding accepts an installation of the GitHub application that this
# SonarQube Cloud instance owns, and of no other application. The server looks
# an installation up in its own records instead of asking GitHub, so an
# installation of another application means nothing to it, however correct the
# identifier looks.
#
# This data source names the application to install.
data "sonarqube_dop_applications" "github" {
  dev_ops_platform = "github"
}

# Install each application from the address below, on the GitHub organization
# that you want to bind.
output "install_the_application_from" {
  value = [
    for application in data.sonarqube_dop_applications.github.applications :
    "https://github.com/apps/${application.application_key}"
  ]
}
