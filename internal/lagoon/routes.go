package lagoon

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/uselagoon/build-deploy-tool/internal/helpers"
	"k8s.io/apimachinery/pkg/util/validation"
)

// RoutesV2 is the new routes definition
type RoutesV2 struct {
	Routes []RouteV2 `json:"routes"`
}

// RouteV2 is the new route definition
type RouteV2 struct {
	Domain                string            `json:"domain"`
	LagoonService         string            `json:"service"`
	ComposeService        string            `json:"composeService"` // the
	TLSAcme               *bool             `json:"tls-acme"`
	Migrate               *bool             `json:"migrate,omitempty"`
	Insecure              *string           `json:"insecure,omitempty"`
	MonitoringPath        string            `json:"monitoring-path,omitempty"`
	Fastly                Fastly            `json:"fastly,omitempty"`
	Annotations           map[string]string `json:"annotations"`
	Labels                map[string]string `json:"labels"`
	AlternativeNames      []string          `json:"alternativeNames"`
	IngressName           string            `json:"ingressName"`
	IngressClass          string            `json:"ingressClass"`
	HSTSEnabled           *bool             `json:"hstsEnabled,omitempty"`
	HSTSMaxAge            int               `json:"hstsMaxAge,omitempty"`
	HSTSIncludeSubdomains *bool             `json:"hstsIncludeSubdomains,omitempty"`
	HSTSPreload           *bool             `json:"hstsPreload,omitempty"`
	Autogenerated         bool              `json:"-"`
	Wildcard              *bool             `json:"wildcard,omitempty"`
	RequestVerification   *bool             `json:"disableRequestVerification,omitempty"`
	PathRoutes            []PathRoute       `json:"pathRoutes,omitempty"`
}

// Ingress represents a Lagoon route.
type Ingress struct {
	TLSAcme               *bool             `json:"tls-acme,omitempty"`
	Migrate               *bool             `json:"migrate,omitempty"`
	Insecure              *string           `json:"insecure,omitempty"`
	MonitoringPath        string            `json:"monitoring-path,omitempty"`
	Fastly                Fastly            `json:"fastly,omitempty"`
	Annotations           map[string]string `json:"annotations,omitempty"`
	IngressClass          string            `json:"ingressClass"`
	HSTSEnabled           *bool             `json:"hstsEnabled,omitempty"`
	HSTSMaxAge            int               `json:"hstsMaxAge,omitempty"`
	HSTSIncludeSubdomains *bool             `json:"hstsIncludeSubdomains,omitempty"`
	HSTSPreload           *bool             `json:"hstsPreload,omitempty"`
	AlternativeNames      []string          `json:"alternativenames,omitempty"`
	Wildcard              *bool             `json:"wildcard,omitempty"`
	RequestVerification   *bool             `json:"disableRequestVerification,omitempty"`
	PathRoutes            []PathRoute       `json:"pathRoutes,omitempty"`
}

// Route can be either a string or a map[string]Ingress, so we must
// implement a custom unmarshaller.
type Route struct {
	Name      string
	Ingresses map[string]Ingress
}

type PathRoute struct {
	ToService string `json:"toService"`
	Path      string `json:"path"`
}

// defaults
var (
	defaultHSTSMaxAge                            = 31536000
	defaultMonitoringPath      string            = "/"
	defaultFastlyService       string            = ""
	defaultFastlyWatch         bool              = false
	defaultInsecure            *string           = helpers.StrPtr("Redirect")
	defaultTLSAcme             *bool             = helpers.BoolPtr(true)
	defaultActiveStandby       *bool             = helpers.BoolPtr(true)
	defaultRequestVerification *bool             = helpers.BoolPtr(false)
	defaultAnnotations         map[string]string = map[string]string{}
)

// UnmarshalJSON implements json.Unmarshaler.
func (r *Route) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, &r.Name); err == nil {
		return nil
	}
	if err := json.Unmarshal(data, &r.Ingresses); err != nil {
		// @TODO: eventually lagoon should be more strict, but in lagoonyaml version 2 we could do this
		// some things in .lagoon.yml can be defined as a bool or string and lagoon builds don't care
		// but types are more strict, so this unmarshaler attempts to change between the two types
		// that can be bool or string
		tmpMap := map[string]interface{}{}
		json.Unmarshal(data, &tmpMap)
		for k := range tmpMap {
			if _, ok := tmpMap[k].(map[string]interface{})["tls-acme"]; ok {
				if reflect.TypeOf(tmpMap[k].(map[string]interface{})["tls-acme"]).Kind() == reflect.String {
					vBool, err := strconv.ParseBool(tmpMap[k].(map[string]interface{})["tls-acme"].(string))
					if err == nil {
						tmpMap[k].(map[string]interface{})["tls-acme"] = vBool
					}
				}
			}
			if _, ok := tmpMap[k].(map[string]interface{})["fastly"]; ok {
				if reflect.TypeOf(tmpMap[k].(map[string]interface{})["fastly"].(map[string]interface{})["watch"]).Kind() == reflect.String {
					vBool, err := strconv.ParseBool(tmpMap[k].(map[string]interface{})["fastly"].(map[string]interface{})["watch"].(string))
					if err == nil {
						tmpMap[k].(map[string]interface{})["fastly"].(map[string]interface{})["watch"] = vBool
					}
				}
			}
		}
		newData, _ := json.Marshal(tmpMap)
		return json.Unmarshal(newData, &r.Ingresses)
	}
	return json.Unmarshal(data, &r.Ingresses)
}

// GenerateRoutesV2 generate routesv2 definitions from lagoon route mappings
func GenerateRoutesV2(yamlRoutes *RoutesV2, routeMap map[string][]Route, variables []EnvironmentVariable, defaultIngressClass string, activeStandby bool) error {
	for rName, lagoonRoutes := range routeMap {
		for _, lagoonRoute := range lagoonRoutes {
			newRoute := RouteV2{}
			// set the defaults for routes
			newRoute.TLSAcme = defaultTLSAcme
			newRoute.Insecure = defaultInsecure
			newRoute.MonitoringPath = defaultMonitoringPath
			newRoute.Annotations = defaultAnnotations
			newRoute.Fastly.ServiceID = defaultFastlyService
			newRoute.Fastly.Watch = defaultFastlyWatch
			newRoute.AlternativeNames = []string{}
			newRoute.IngressClass = defaultIngressClass
			newRoute.RequestVerification = defaultRequestVerification
			if activeStandby {
				newRoute.Migrate = defaultActiveStandby
			}
			if lagoonRoute.Name == "" {
				// this route from the lagoon route map contains field overrides
				// update them from the defaults in this case
				for iName, ingress := range lagoonRoute.Ingresses {
					newRoute.Domain = iName
					newRoute.LagoonService = rName
					newRoute.IngressName = iName
					newRoute.IngressClass = defaultIngressClass
					newRoute.Fastly = ingress.Fastly
					if ingress.Annotations != nil {
						newRoute.Annotations = ingress.Annotations
					}
					if ingress.TLSAcme != nil {
						newRoute.TLSAcme = ingress.TLSAcme
					}
					if ingress.Insecure != nil {
						newRoute.Insecure = ingress.Insecure
					}
					if ingress.AlternativeNames != nil {
						newRoute.AlternativeNames = ingress.AlternativeNames
					}
					if ingress.IngressClass != "" {
						newRoute.IngressClass = ingress.IngressClass
					}
					if ingress.MonitoringPath != "" {
						newRoute.MonitoringPath = ingress.MonitoringPath
					}

					// handle hsts here
					if ingress.HSTSEnabled != nil {
						newRoute.HSTSEnabled = ingress.HSTSEnabled
					}
					if ingress.HSTSIncludeSubdomains != nil {
						newRoute.HSTSIncludeSubdomains = ingress.HSTSIncludeSubdomains
					}
					if ingress.HSTSPreload != nil {
						newRoute.HSTSPreload = ingress.HSTSPreload
					}
					if ingress.HSTSMaxAge > 0 {
						newRoute.HSTSMaxAge = ingress.HSTSMaxAge
					} else {
						if newRoute.HSTSEnabled != nil && *newRoute.HSTSEnabled {
							newRoute.HSTSMaxAge = defaultHSTSMaxAge // set default hsts value if one not provided
						}
					}
					// hsts end

					// handle wildcards
					if ingress.Wildcard != nil {
						newRoute.Wildcard = ingress.Wildcard
						if *newRoute.TLSAcme == true && *newRoute.Wildcard == true {
							return fmt.Errorf("Route %s has wildcard: true and tls-acme: true, this is not supported", newRoute.Domain)
						}
						if ingress.AlternativeNames != nil && *newRoute.Wildcard == true {
							return fmt.Errorf("Route %s has wildcard: true and alternativenames defined, this is not supported", newRoute.Domain)
						}
						newRoute.IngressName = fmt.Sprintf("wildcard-%s", newRoute.Domain)
						if err := validation.IsDNS1123Subdomain(strings.ToLower(newRoute.IngressName)); err != nil {
							newRoute.IngressName = fmt.Sprintf("%s-%s", newRoute.IngressName[:len(newRoute.IngressName)-10], helpers.GetMD5HashWithNewLine(newRoute.Domain)[:5])
						}
					}

					// aergia verification disabling
					if ingress.RequestVerification != nil {
						newRoute.RequestVerification = ingress.RequestVerification
					}

					// path based routing
					if ingress.PathRoutes != nil {
						newRoute.PathRoutes = ingress.PathRoutes
					}
				}
			} else {
				// this route is just a domain
				// keep the defaults, just set the name and service
				newRoute.Domain = lagoonRoute.Name
				newRoute.LagoonService = rName
				newRoute.IngressName = lagoonRoute.Name
			}
			// generate the fastly configuration for this route
			err := GenerateFastlyConfiguration(&newRoute.Fastly, "", newRoute.Fastly.ServiceID, newRoute.Domain, variables)
			if err != nil {
				//@TODO: error handling
			}

			// validate the domain earlier and fail if it is invalid
			if err := validation.IsDNS1123Subdomain(strings.ToLower(newRoute.Domain)); err != nil {
				return fmt.Errorf("Route %s in .lagoon.yml is not valid: %v", newRoute.Domain, err)
			}
			yamlRoutes.Routes = append(yamlRoutes.Routes, newRoute)
		}
	}
	return nil
}

// MergeRoutesV2 merge routes from the API onto the previously generated routes.
func MergeRoutesV2(yamlRoutes RoutesV2, apiRoutes RoutesV2, variables []EnvironmentVariable, defaultIngressClass string) (RoutesV2, error) {
	firstRoundRoutes := RoutesV2{}
	existsInAPI := false
	// replace any routes from the lagoon yaml with ones from the api
	// this only modifies ones that exist in lagoon yaml
	for _, route := range yamlRoutes.Routes {
		routeAdd := RouteV2{}
		// validate the domain earlier and fail if it is invalid
		if err := validation.IsDNS1123Subdomain(strings.ToLower(route.Domain)); err != nil {
			return firstRoundRoutes, fmt.Errorf("Route %s in .lagoon.yml is not valid: %v", route.Domain, err)
		}
		for _, apiRoute := range apiRoutes.Routes {
			if apiRoute.Domain == route.Domain {
				// validate the domain earlier and fail if it is invalid
				if err := validation.IsDNS1123Subdomain(strings.ToLower(apiRoute.Domain)); err != nil {
					return firstRoundRoutes, fmt.Errorf("Route %s in API defined routes is not valid: %v", apiRoute.Domain, err)
				}
				existsInAPI = true
				var err error
				routeAdd, err = handleAPIRoute(defaultIngressClass, apiRoute)
				if err != nil {
					return firstRoundRoutes, err
				}
			}
		}
		if existsInAPI {
			firstRoundRoutes.Routes = append(firstRoundRoutes.Routes, routeAdd)
			existsInAPI = false
		} else {

			if route.AlternativeNames == nil {
				route.AlternativeNames = []string{}
			}
			firstRoundRoutes.Routes = append(firstRoundRoutes.Routes, route)
		}
	}
	// add any that exist in the api only to the final routes list
	for _, apiRoute := range apiRoutes.Routes {
		if err := validation.IsDNS1123Subdomain(strings.ToLower(apiRoute.Domain)); err != nil {
			return firstRoundRoutes, fmt.Errorf("Route %s in API defined routes is not valid: %v", apiRoute.Domain, err)
		}

		routeAdd, err := handleAPIRoute(defaultIngressClass, apiRoute)
		if err != nil {
			return firstRoundRoutes, err
		}

		for _, route := range firstRoundRoutes.Routes {
			if apiRoute.Domain == route.Domain {
				existsInAPI = true
			}
		}
		if existsInAPI {
			existsInAPI = false
		} else {
			firstRoundRoutes.Routes = append(firstRoundRoutes.Routes, routeAdd)
		}
	}

	// generate the final routes to provide back as "the" route list for this environment
	finalRoutes := RoutesV2{}
	for _, fRoute := range firstRoundRoutes.Routes {
		// generate the fastly configuration for this route if required
		err := GenerateFastlyConfiguration(&fRoute.Fastly, "", fRoute.Fastly.ServiceID, fRoute.Domain, variables)
		if err != nil {
			//@TODO: error handling
		}
		fRoute.Domain = strings.ToLower(fRoute.Domain)
		finalRoutes.Routes = append(finalRoutes.Routes, fRoute)
	}
	return finalRoutes, nil
}

// handleAPIRoute handles setting the defaults for API defined routes
// main lagoon.yml defaults are handled in `GenerateRoutesV2` function
func handleAPIRoute(defaultIngressClass string, apiRoute RouteV2) (RouteV2, error) {
	routeAdd := apiRoute
	// copy in the apiroute fastly configuration
	routeAdd.Fastly = apiRoute.Fastly

	routeAdd.IngressName = apiRoute.Domain
	if apiRoute.TLSAcme != nil {
		routeAdd.TLSAcme = apiRoute.TLSAcme
	} else {
		routeAdd.TLSAcme = defaultTLSAcme
	}
	if apiRoute.Insecure != nil {
		routeAdd.Insecure = apiRoute.Insecure
	} else {
		routeAdd.Insecure = defaultInsecure
	}
	if apiRoute.Annotations != nil {
		routeAdd.Annotations = apiRoute.Annotations
	} else {
		routeAdd.Annotations = defaultAnnotations
	}
	if apiRoute.AlternativeNames != nil {
		routeAdd.AlternativeNames = apiRoute.AlternativeNames
	} else {
		routeAdd.AlternativeNames = []string{}
	}
	if apiRoute.IngressClass != "" {
		routeAdd.IngressClass = apiRoute.IngressClass
	} else {
		routeAdd.IngressClass = defaultIngressClass
	}

	// handle hsts here
	if apiRoute.HSTSEnabled != nil {
		routeAdd.HSTSEnabled = apiRoute.HSTSEnabled
	}
	if apiRoute.HSTSIncludeSubdomains != nil {
		routeAdd.HSTSIncludeSubdomains = apiRoute.HSTSIncludeSubdomains
	}
	if apiRoute.HSTSPreload != nil {
		routeAdd.HSTSPreload = apiRoute.HSTSPreload
	}
	if apiRoute.HSTSMaxAge > 0 {
		routeAdd.HSTSMaxAge = apiRoute.HSTSMaxAge
	} else {
		if routeAdd.HSTSEnabled != nil && *routeAdd.HSTSEnabled {
			routeAdd.HSTSMaxAge = defaultHSTSMaxAge // set default hsts value if one not provided
		}
	}
	// hsts end

	// handle wildcards
	if apiRoute.Wildcard != nil {
		routeAdd.Wildcard = apiRoute.Wildcard
		if *routeAdd.TLSAcme == true && *routeAdd.Wildcard == true {
			return routeAdd, fmt.Errorf("Route %s has wildcard=true and tls-acme=true, this is not supported", routeAdd.Domain)
		}
		if apiRoute.AlternativeNames != nil && *routeAdd.Wildcard == true {
			return routeAdd, fmt.Errorf("Route %s has wildcard=true and alternativenames defined, this is not supported", routeAdd.Domain)
		}
		apiRoute.IngressName = fmt.Sprintf("wildcard-%s", apiRoute.Domain)
		if err := validation.IsDNS1123Subdomain(strings.ToLower(apiRoute.IngressName)); err != nil {
			apiRoute.IngressName = fmt.Sprintf("%s-%s", apiRoute.IngressName[:len(apiRoute.IngressName)-10], helpers.GetMD5HashWithNewLine(apiRoute.Domain)[:5])
		}
	}

	if apiRoute.RequestVerification != nil {
		routeAdd.RequestVerification = apiRoute.RequestVerification
	} else {
		routeAdd.RequestVerification = defaultRequestVerification
	}

	// path based routing
	if apiRoute.PathRoutes != nil {
		routeAdd.PathRoutes = apiRoute.PathRoutes
	}
	return routeAdd, nil
}
