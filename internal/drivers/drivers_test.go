package drivers

import "testing"

// TestEveryDriverHasAConnectionTest garantit qu'un driver ajouté aux tables
// de constructeurs (scmDrivers, ciDrivers, ...) ne peut pas être proposé par
// le wizard (AvailableTypes) sans avoir son smoke test correspondant dans
// TestProvider — sinon le bouton "Tester la connexion" échouerait
// silencieusement avec "type inconnu" pour un type pourtant listé.
func TestEveryDriverHasAConnectionTest(t *testing.T) {
	cases := []struct {
		category string
		drivers  []string
		tests    map[string]func(map[string]string) (string, error)
	}{
		{"scm", keys(scmDrivers), scmTests},
		{"ci", keys(ciDrivers), ciTests},
		{"registry", keys(registryDrivers), registryTests},
		{"deploy", keys(deployDrivers), deployTests},
	}
	for _, c := range cases {
		for _, driverType := range c.drivers {
			if _, ok := c.tests[driverType]; !ok {
				t.Errorf("%s: type %q compilé (voir AvailableTypes) mais sans test de connexion associé dans TestProvider", c.category, driverType)
			}
		}
	}
}
