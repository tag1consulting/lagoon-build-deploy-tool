package lagoon

import (
	"fmt"
	"strconv"
	"strings"
)

// Fastly represents the fastly configuration for a Lagoon route
type Fastly struct {
	ServiceID     string `json:"service-id,omitempty"`
	APISecretName string `json:"api-secret-name,omitempty"`
	Watch         bool   `json:"watch,omitempty"`
}

// GenerateFastlyConfiguration generates the fastly configuration for a specific route from Lagoon variables.
func GenerateFastlyConfiguration(f *Fastly, noCacheServiceID, serviceID, route, secretPrefix string, variables []EnvironmentVariable) error {
	f.ServiceID = serviceID
	if serviceID == "" {
		if noCacheServiceID != "" {
			f.ServiceID = noCacheServiceID
			f.Watch = true
		}
	}
	// check lagoon api variables for `LAGOON_FASTLY_SERVICE_ID`
	// this is supported as `SERVICE_ID:WATCH_STATUS:SECRET_NAME(optional)` eg: "fa23rsdgsdgas:false", "fa23rsdgsdgas:true" or "fa23rsdgsdgas:true:examplecom"
	// this will apply to ALL ingresses if one is not specifically defined in the `LAGOON_FASTLY_SERVICE_IDS` environment variable override
	// see section `FASTLY SERVICE ID PER INGRESS OVERRIDE` in `build-deploy-docker-compose.sh` for info on `LAGOON_FASTLY_SERVICE_IDS`
	lfsID, err := GetLagoonVariable("LAGOON_FASTLY_SERVICE_ID", []string{"build", "global"}, variables)
	if err == nil {
		lfsIDSplit := strings.Split(lfsID.Value, ":")
		if len(lfsIDSplit) == 1 {
			return fmt.Errorf("no watch status was provided, only the service id")
		}
		watch, err := strconv.ParseBool(lfsIDSplit[1])
		if err != nil {
			return fmt.Errorf("the provided value %s is not a valid boolean", lfsIDSplit[1])
		}
		f.ServiceID = lfsIDSplit[0]
		f.Watch = watch
		if len(lfsIDSplit) == 3 {
			// the optional secret has been defined
			f.APISecretName = fmt.Sprintf("%s%s", secretPrefix, lfsIDSplit[2])
		}
	}
	// check the `LAGOON_FASTLY_SERVICE_IDS` to see if we have a domain specific override
	// this is useful if all domains are using the nocache service, but you have a specific domain that should use a different service
	// and you haven't defined it in the lagoon.yml file
	// # FASTLY SERVICE ID PER INGRESS OVERRIDE FROM LAGOON API VARIABLE
	// # Allow the fastly serviceid for specific ingress to be overridden by the lagoon API
	// # This accepts colon separated values like so `INGRESS_DOMAIN:FASTLY_SERVICE_ID:WATCH_STATUS:SECRET_NAME(OPTIONAL)`, and multiple overrides
	// # separated by commas
	// # Example 1: www.example.com:x1s8asfafasf7ssf:true
	// # ^^^ tells the ingress creation to use the service id x1s8asfafasf7ssf for ingress www.example.com, with the watch status of true
	// # Example 2: www.example.com:x1s8asfafasf7ssf:true,www.not-example.com:fa23rsdgsdgas:false
	// # ^^^ same as above, but also tells the ingress creation to use the service id fa23rsdgsdgas for ingress www.not-example.com, with the watch status of false
	// # Example 3: www.example.com:x1s8asfafasf7ssf:true:examplecom
	// # ^^^ tells the ingress creation to use the service id x1s8asfafasf7ssf for ingress www.example.com, with the watch status of true
	// # but it will also be annotated to be told to use the secret named `examplecom` that could be defined elsewhere
	lfsIDs, err := GetLagoonVariable("LAGOON_FASTLY_SERVICE_IDS", []string{"build", "global"}, variables)
	if err == nil {
		lfsIDsSplit := strings.Split(lfsIDs.Value, ",")
		for _, lfs := range lfsIDsSplit {
			lfsIDSplit := strings.Split(lfs, ":")
			if lfsIDSplit[0] == route {
				if len(lfsIDSplit) == 2 {
					return fmt.Errorf("no watch status was provided, only the route and service id")
				}
				watch, err := strconv.ParseBool(lfsIDSplit[2])
				if err != nil {
					return fmt.Errorf("the provided value %s is not a valid boolean", lfsIDSplit[2])
				}
				f.ServiceID = lfsIDSplit[1]
				f.Watch = watch
				// unset the apisecret name if this point is reached
				// this is because this particular ingress may not have one defined
				// it will get checked next
				f.APISecretName = ""
				if len(lfsIDSplit) == 4 {
					// the optional secret has been defined
					f.APISecretName = fmt.Sprintf("%s%s", secretPrefix, lfsIDSplit[3])
				}
			}
		}
	}
	if f.APISecretName != "" {
		if !strings.HasPrefix(f.APISecretName, secretPrefix) {
			f.APISecretName = fmt.Sprintf("%s%s", secretPrefix, f.APISecretName)
		}
	}
	return nil
}
