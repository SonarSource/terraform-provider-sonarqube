# The resource owns all conditions of the gate. An apply deletes a condition
# that is not in the configuration, also when someone adds it in the UI.
resource "sonarqube_cloud_quality_gate" "example" {
  organization = "my-organization"
  name         = "My quality gate"

  # The organization must have the AI Code Assurance feature to set true.
  ai_code_assurance = false

  condition {
    metric    = "new_coverage"
    operator  = "LT"
    threshold = "80"
  }

  condition {
    metric    = "new_duplicated_lines_density"
    operator  = "GT"
    threshold = "3"
  }
}
