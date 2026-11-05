package acctest

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/SonarSource/terraform-provider-sonarqube/internal/client"
	"github.com/SonarSource/terraform-provider-sonarqube/internal/provider"
)

// item is one thing that a sweeper can delete.
type item struct {
	id   string
	name string
}

// sweeper deletes each item of one kind whose name starts with KeyPrefix.
// list returns the candidates and leaves out what the server cannot delete,
// such as a default. A list can first move a default with the prefix out of
// the way, so that its item can be deleted. Each list but the one of the organizations reads the
// shared organization of EnvTestOrganization: the organizations that the tests
// make hold their own items, and their deletion deletes those items.
type sweeper struct {
	name string
	// dependencies run first. A project can use a gate.
	dependencies []string
	shared       bool
	list         func(ctx context.Context, c *client.Client, organization string) ([]item, error)
	delete       func(ctx context.Context, c *client.Client, organization string, it item) error
}

var sweepers = []sweeper{
	{name: "sonarqube_cloud_scoped_organization_token", shared: true,
		list: func(ctx context.Context, c *client.Client, organization string) ([]item, error) {
			org, err := c.GetOrganization(ctx, organization)
			if err != nil {
				return nil, err
			}
			tokens, err := c.ListOrganizationTokens(ctx, org.UUIDV4)
			if err != nil {
				return nil, err
			}
			items := make([]item, 0, len(tokens))
			for _, token := range tokens {
				items = append(items, item{id: token.ID, name: token.Name})
			}
			return items, nil
		},
		delete: func(ctx context.Context, c *client.Client, organization string, it item) error {
			org, err := c.GetOrganization(ctx, organization)
			if err != nil {
				return err
			}
			return c.DeleteOrganizationToken(ctx, org.UUIDV4, it.id)
		},
	},
	{
		// The organizations of a deleted enterprise stay. The license of the
		// enterprise is used up in all cases. The organization sweeper does not
		// depend on this one, so that a token without access to the enterprise
		// API does not stop it.
		name: "sonarqube_cloud_enterprise",
		list: func(ctx context.Context, c *client.Client, _ string) ([]item, error) {
			enterprises, err := c.ListEnterprises(ctx)
			items := make([]item, len(enterprises))
			for i, enterprise := range enterprises {
				items[i] = item{id: enterprise.ID, name: enterprise.Key}
			}
			return items, err
		},
		delete: func(ctx context.Context, c *client.Client, _ string, it item) error {
			return c.DeleteEnterprise(ctx, it.id)
		},
	},
	{
		name: "sonarqube_cloud_organization",
		// The shared organization stays, also when a wrong setup gives it
		// the prefix: its deletion would delete all that the tests need.
		list: func(ctx context.Context, c *client.Client, organization string) ([]item, error) {
			keys, err := c.ListMemberOrganizationKeys(ctx)
			keys = slices.DeleteFunc(keys, func(key string) bool { return key == organization })
			return keyItems(keys), err
		},
		delete: func(ctx context.Context, c *client.Client, _ string, it item) error {
			return c.DeleteOrganization(ctx, it.id)
		},
	},
	{
		name:   "sonarqube_project",
		shared: true,
		list: func(ctx context.Context, c *client.Client, organization string) ([]item, error) {
			keys, err := c.SearchProjectKeys(ctx, organization, KeyPrefix)
			return keyItems(keys), err
		},
		delete: func(ctx context.Context, c *client.Client, _ string, it item) error {
			return c.DeleteProject(ctx, it.id)
		},
	},
	{
		// A delete also deletes the descendants, so the delete of a child
		// after its parent finds nothing, which is success. A default with
		// the prefix gives its place to a built-in profile first. The server
		// refuses to delete a default profile or one of its ancestors, and
		// the delete of an ancestor of a profile without the prefix would
		// delete that profile too.
		name:   "sonarqube_quality_profile",
		shared: true,
		list: func(ctx context.Context, c *client.Client, organization string) ([]item, error) {
			profiles, err := c.ListQualityProfiles(ctx, organization, "")
			if err != nil {
				return nil, err
			}
			// A profile that a profile without the prefix extends stays, so its
			// default does not move.
			extended := ancestorsOf(profiles, func(profile client.QualityProfile) bool {
				return !strings.HasPrefix(profile.Name, KeyPrefix)
			})
			replaced := replaceTestDefaults(ctx, c, organization, profiles, extended)
			isDefault := func(profile client.QualityProfile) bool { return profile.IsDefault && !replaced[profile.Key] }
			ancestors := ancestorsOf(profiles, func(profile client.QualityProfile) bool {
				return isDefault(profile) || !strings.HasPrefix(profile.Name, KeyPrefix)
			})
			var items []item
			for _, profile := range profiles {
				switch {
				case profile.IsBuiltIn || !strings.HasPrefix(profile.Name, KeyPrefix):
				case extended[profile.Key]:
					log.Printf("[WARN] Keeping quality profile %s of %s: a profile without the prefix %s extends it. "+
						"Change the parent of that profile, then run the sweeper again", profile.Name, organization, KeyPrefix)
				case isDefault(profile):
					logKeptDefault("quality profile", profile.Name, organization)
				case ancestors[profile.Key]:
					log.Printf("[WARN] Keeping quality profile %s of %s, an ancestor of a default profile or of "+
						"a profile without the prefix %s", profile.Name, organization, KeyPrefix)
				default:
					items = append(items, item{id: profile.Key, name: profile.Name})
				}
			}
			return items, nil
		},
		delete: func(ctx context.Context, c *client.Client, organization string, it item) error {
			return c.DeleteQualityProfile(ctx, it.id)
		},
	},
	{
		name:   "sonarqube_group",
		shared: true,
		list: func(ctx context.Context, c *client.Client, organization string) ([]item, error) {
			groups, err := c.SearchGroups(ctx, organization, KeyPrefix)
			items := make([]item, len(groups))
			for i, group := range groups {
				items[i] = item{id: group.ID, name: group.Name}
			}
			return items, err
		},
		delete: func(ctx context.Context, c *client.Client, _ string, it item) error {
			return c.DeleteGroup(ctx, it.id)
		},
	},
	{
		name:         "sonarqube_cloud_quality_gate",
		dependencies: []string{"sonarqube_project"},
		shared:       true,
		list: func(ctx context.Context, c *client.Client, organization string) ([]item, error) {
			org, err := c.GetOrganization(ctx, organization)
			if err != nil {
				return nil, err
			}
			gates, err := c.ListQualityGates(ctx, org.UUIDV4)
			if err != nil {
				return nil, err
			}
			var items []item
			for _, gate := range gates {
				if gate.ID == org.DefaultQualityGateUUID {
					logKeptDefault("quality gate", gate.Name, organization)
					continue
				}
				items = append(items, item{id: gate.ID, name: gate.Name})
			}
			return items, nil
		},
		delete: func(ctx context.Context, c *client.Client, _ string, it item) error {
			return c.DeleteQualityGate(ctx, it.id)
		},
	},
}

// The sweepers run when a test package calls resource.TestMain from its
// TestMain and the run has -sweep. Each test package imports this package, so
// each one has them. A sweeper does not run when one of its dependencies
// fails, unless the run also has -sweep-allow-failures.
func init() {
	for _, s := range sweepers {
		resource.AddTestSweepers(s.name, &resource.Sweeper{
			Name:         s.name,
			Dependencies: s.dependencies,
			F:            func(string) error { return s.run(context.Background()) },
		})
	}
}

func (s sweeper) run(ctx context.Context) error {
	c, err := sweepClient()
	if err != nil {
		return err
	}
	organization := os.Getenv(EnvTestOrganization)
	if s.shared && organization == "" {
		return fmt.Errorf("%s is not set, so the sweeper has no shared organization", EnvTestOrganization)
	}
	items, err := s.list(ctx, c, organization)
	if err != nil {
		return err
	}
	var errs []error
	for _, it := range items {
		if !strings.HasPrefix(it.name, KeyPrefix) {
			continue
		}
		log.Printf("[INFO] %s: deleting %s", s.name, it.name)
		if err := s.delete(ctx, c, organization, it); err != nil {
			errs = append(errs, fmt.Errorf("%s %s: %w", s.name, it.name, err))
		}
	}
	return errors.Join(errs...)
}

// sweepClient returns a client for the instance of the environment. It
// refuses the instances where no test may delete, because a sweeper deletes
// more than a test does.
func sweepClient() (*client.Client, error) {
	if os.Getenv(provider.EnvToken) == "" {
		return nil, fmt.Errorf("%s is not set, so the sweeper cannot reach an instance", provider.EnvToken)
	}
	instanceURL := os.Getenv(provider.EnvURL)
	if instanceURL == "" {
		return nil, fmt.Errorf("%s is not set. The sweeper deletes, so it must name its instance", provider.EnvURL)
	}
	c := Client()
	if err := requireSafeClient(c); err != nil {
		return nil, err
	}
	return c, nil
}

// ancestorsOf returns the keys of the parents, grandparents and so on of each
// profile that keep accepts.
func ancestorsOf(profiles []client.QualityProfile, keep func(client.QualityProfile) bool) map[string]bool {
	parents := map[string]string{}
	for _, profile := range profiles {
		parents[profile.Key] = profile.ParentKey
	}
	ancestors := map[string]bool{}
	for _, profile := range profiles {
		if !keep(profile) {
			continue
		}
		for key := profile.ParentKey; key != "" && !ancestors[key]; key = parents[key] {
			ancestors[key] = true
		}
	}
	return ancestors
}

// builtInDefaultNames are the built-in profiles that take the place of a
// default profile with the prefix, in the order of preference. The other
// built-in profiles of the language follow them.
var builtInDefaultNames = []string{"Sonar way", "Sonar way comprehensive"}

// replaceTestDefaults makes a built-in profile the default of each language
// whose default has the prefix. It tries the next built-in profile when the
// server refuses one, for example a MISRA profile without the entitlement. It
// returns the keys of the profiles that are no longer the default. It skips
// the profiles in keep.
func replaceTestDefaults(ctx context.Context, c *client.Client, organization string, profiles []client.QualityProfile, keep map[string]bool) map[string]bool {
	replaced := map[string]bool{}
	for _, profile := range profiles {
		if !profile.IsDefault || profile.IsBuiltIn || keep[profile.Key] || !strings.HasPrefix(profile.Name, KeyPrefix) {
			continue
		}
		for _, builtIn := range builtInDefaults(profiles, profile.Language) {
			if err := c.SetDefaultQualityProfile(ctx, builtIn.Key); err != nil {
				log.Printf("[WARN] Cannot make %s the default quality profile of %s in %s: %v",
					builtIn.Name, profile.Language, organization, err)
				continue
			}
			log.Printf("[INFO] Made %s the default quality profile of %s in %s in place of %s",
				builtIn.Name, profile.Language, organization, profile.Name)
			replaced[profile.Key] = true
			break
		}
	}
	return replaced
}

// builtInDefaults returns the built-in profiles of the language: first the
// ones of builtInDefaultNames in that order, then the others.
func builtInDefaults(profiles []client.QualityProfile, language string) []client.QualityProfile {
	var preferred, others []client.QualityProfile
	for _, name := range builtInDefaultNames {
		for _, profile := range profiles {
			if profile.IsBuiltIn && profile.Language == language && profile.Name == name {
				preferred = append(preferred, profile)
			}
		}
	}
	for _, profile := range profiles {
		if profile.IsBuiltIn && profile.Language == language && !slices.Contains(builtInDefaultNames, profile.Name) {
			others = append(others, profile)
		}
	}
	return append(preferred, others...)
}

// keyItems makes items of the things that a key names. The key is also the
// name.
func keyItems(keys []string) []item {
	items := make([]item, len(keys))
	for i, key := range keys {
		items[i] = item{id: key, name: key}
	}
	return items
}

// logKeptDefault reports a default that a crashed test left. The server cannot
// delete it, so the sweeper keeps it, and the sweepers that depend on this one
// still run.
func logKeptDefault(kind, name, organization string) {
	if strings.HasPrefix(name, KeyPrefix) {
		log.Printf("[WARN] Keeping %s %s, the default of %s. Set a different default, "+
			"then run the sweeper again", kind, name, organization)
	}
}
