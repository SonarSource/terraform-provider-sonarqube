# Products of each resource and data source

The products that each resource and data source supports, and why. The rules
are in the "SonarQube Cloud and SonarQube Server" section of `AGENTS.md`.
Add a row before you add a resource or data source, and update a row when a
decision changes.

The alpha supports SonarQube Cloud only. "Shared" means that the resource
will also work on SonarQube Server when the provider supports it.

## Resources

| Resource | Products | Reason |
|---|---|---|
| `sonarqube_project` | Shared | Only the scope is different. SonarQube Server has no organizations, so the provider will refuse `organization` and the import ID will be `key` there. The schema will then make `organization` optional, and the provider will require it on SonarQube Cloud. Both products create and delete a project with the same web service. |
| `sonarqube_cloud_organization` | SonarQube Cloud | SonarQube Server has no organizations. |
| `sonarqube_cloud_organization_quality_gate_settings` | SonarQube Cloud | SonarQube Server has no organization setting to ignore duplication and coverage conditions on small changes. |
| `sonarqube_cloud_organization_binding` | SonarQube Cloud | SonarQube Server has no organizations. It connects to a DevOps platform through integration settings of the instance, which have a different schema. |
| `sonarqube_cloud_project_binding` | SonarQube Cloud | A SonarQube Server binding needs the key of a DevOps platform integration and attributes for each platform. It also has a real delete, but on SonarQube Cloud a destroy only removes the binding from the state. |
| `sonarqube_cloud_quality_gate` | SonarQube Cloud | Cloud gates use organization scope, UUIDs, an AI Code Assurance flag, and condition metric identifiers that Server does not share. The required schema and import ID therefore differ. |
| `sonarqube_cloud_organization_default_quality_gate` | SonarQube Cloud | The default belongs to an organization, which SonarQube Server does not have. On SonarQube Server, the default belongs to the instance, and the import ID is different. |
| `sonarqube_cloud_project_quality_gate` | SonarQube Cloud | The assignment uses organization scope and gate UUIDs, and the import ID is `<organization>/<project_key>`. SonarQube Server has no organizations and names a gate by its name. |

## Data sources

| Data source | Products | Reason |
|---|---|---|
| `sonarqube_cloud_organization` | SonarQube Cloud | SonarQube Server has no organizations. |
| `sonarqube_cloud_organization_binding` | SonarQube Cloud | Same reason as the resource. |
| `sonarqube_cloud_project_binding` | SonarQube Cloud | Same reason as the resource. |
| `sonarqube_cloud_dop_applications` | SonarQube Cloud | It lists the DevOps platform applications of a SonarQube Cloud instance, which an organization binding uses. SonarQube Server keeps DevOps platform integration settings instead, with a different schema. |
| `sonarqube_cloud_quality_gate` | SonarQube Cloud | Same Cloud-specific gate identity and attributes as the resource. |
