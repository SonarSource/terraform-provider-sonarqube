data "sonarqube_group" "members" {
  organization = "my-organization"
  name         = "Members"
}

output "members_group_id" {
  value = data.sonarqube_group.members.id
}
