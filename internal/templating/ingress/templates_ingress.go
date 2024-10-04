package routes

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/uselagoon/build-deploy-tool/internal/generator"
	"github.com/uselagoon/build-deploy-tool/internal/helpers"
	"github.com/uselagoon/build-deploy-tool/internal/lagoon"
	"github.com/uselagoon/build-deploy-tool/internal/templating/services"
	networkv1 "k8s.io/api/networking/v1"
	apivalidation "k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metavalidation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	utilvalidation "k8s.io/apimachinery/pkg/util/validation"

	"sigs.k8s.io/yaml"
)

// GenerateIngressTemplate generates the lagoon template to apply.
func GenerateIngressTemplate(
	route lagoon.RouteV2,
	lValues generator.BuildValues,
) ([]byte, error) {

	// truncate the route for use in labels and secretname
	truncatedRouteDomain := route.Domain
	if len(truncatedRouteDomain) >= 53 {
		subdomain := strings.Split(truncatedRouteDomain, ".")[0]
		if errs := utilvalidation.IsValidLabelValue(subdomain); errs != nil {
			subdomain = subdomain[:53]
		}
		truncatedRouteDomain = fmt.Sprintf("%s-%s", strings.Split(subdomain, ".")[0], helpers.GetMD5HashWithNewLine(route.Domain)[:5])
	}

	// create the ingress object for templating
	ingress := &networkv1.Ingress{}
	ingress.TypeMeta = metav1.TypeMeta{
		Kind:       "Ingress",
		APIVersion: "networking.k8s.io/v1",
	}
	ingress.ObjectMeta.Name = route.IngressName

	// if this is a wildcard ingress, handle templating that here
	if route.Wildcard != nil && *route.Wildcard {
		truncatedRouteDomain = fmt.Sprintf("wildcard-%s", truncatedRouteDomain)
		if len(truncatedRouteDomain) >= 53 {
			subdomain := strings.Split(truncatedRouteDomain, "-")[0]
			if errs := utilvalidation.IsValidLabelValue(subdomain); errs != nil {
				subdomain = subdomain[:53]
			}
			truncatedRouteDomain = fmt.Sprintf("%s-%s", strings.Split(subdomain, "-")[0], helpers.GetMD5HashWithNewLine(route.Domain)[:5])
		}
		// set the domain to include the wildcard prefix
		route.Domain = fmt.Sprintf("*.%s", route.Domain)
	}

	// add the default labels
	ingress.ObjectMeta.Labels = map[string]string{
		"lagoon.sh/autogenerated":      "false",
		"app.kubernetes.io/name":       "custom-ingress",
		"app.kubernetes.io/instance":   truncatedRouteDomain,
		"app.kubernetes.io/managed-by": "build-deploy-tool",
		"lagoon.sh/template":           "custom-ingress-0.1.0",
		"lagoon.sh/service":            truncatedRouteDomain,
		"lagoon.sh/service-type":       "custom-ingress",
		"lagoon.sh/project":            lValues.Project,
		"lagoon.sh/environment":        lValues.Environment,
		"lagoon.sh/environmentType":    lValues.EnvironmentType,
		"lagoon.sh/buildType":          lValues.BuildType,
	}
	additionalLabels := map[string]string{}

	// add the default annotations
	ingress.ObjectMeta.Annotations = map[string]string{
		"kubernetes.io/tls-acme": strconv.FormatBool(*route.TLSAcme),
		"fastly.amazee.io/watch": strconv.FormatBool(route.Fastly.Watch),
		"lagoon.sh/version":      lValues.LagoonVersion,
	}
	additionalAnnotations := map[string]string{}

	if lValues.EnvironmentType == "production" && !route.Autogenerated {
		if route.Migrate != nil {
			additionalLabels["activestandby.lagoon.sh/migrate"] = strconv.FormatBool(*route.Migrate)
		} else {
			additionalLabels["activestandby.lagoon.sh/migrate"] = "false"
		}
	}
	if lValues.EnvironmentType == "production" {
		// monitoring is only available in production environments
		additionalAnnotations["monitor.stakater.com/enabled"] = "false"
		primaryIngress, _ := url.Parse(lValues.Route)
		// check if monitoring enabled, route isn't autogenerated, and the primary ingress from the .lagoon.yml is this processed routedomain
		// and enable monitoring on the primary ingress only.
		if lValues.Monitoring.Enabled && !route.Autogenerated && primaryIngress.Host == route.Domain {
			additionalLabels["lagoon.sh/primaryIngress"] = "true"

			// only add the monitring annotations if monitoring is enabled
			additionalAnnotations["monitor.stakater.com/enabled"] = "true"
			additionalAnnotations["uptimerobot.monitor.stakater.com/alert-contacts"] = "unconfigured"
			if lValues.Monitoring.AlertContact != "" {
				additionalAnnotations["uptimerobot.monitor.stakater.com/alert-contacts"] = lValues.Monitoring.AlertContact
			}
			if lValues.Monitoring.StatusPageID != "" {
				additionalAnnotations["uptimerobot.monitor.stakater.com/status-pages"] = lValues.Monitoring.StatusPageID
			}
			additionalAnnotations["uptimerobot.monitor.stakater.com/interval"] = "60"
		}
		if route.MonitoringPath != "" {
			additionalAnnotations["monitor.stakater.com/overridePath"] = route.MonitoringPath
		}
	}
	if route.Fastly.ServiceID != "" {
		additionalAnnotations["fastly.amazee.io/service-id"] = route.Fastly.ServiceID
	}
	if lValues.BuildType == "branch" {
		additionalAnnotations["lagoon.sh/branch"] = lValues.Branch
	} else if lValues.BuildType == "pullrequest" {
		additionalAnnotations["lagoon.sh/prNumber"] = lValues.PRNumber
		additionalAnnotations["lagoon.sh/prHeadBranch"] = lValues.PRHeadBranch
		additionalAnnotations["lagoon.sh/prBaseBranch"] = lValues.PRBaseBranch

	}
	if *route.Insecure == "Allow" {
		additionalAnnotations["nginx.ingress.kubernetes.io/ssl-redirect"] = "false"
		additionalAnnotations["ingress.kubernetes.io/ssl-redirect"] = "false"
	} else if *route.Insecure == "Redirect" || *route.Insecure == "None" {
		additionalAnnotations["nginx.ingress.kubernetes.io/ssl-redirect"] = "true"
		additionalAnnotations["ingress.kubernetes.io/ssl-redirect"] = "true"
	}
	if lValues.EnvironmentType == "development" || route.Autogenerated {
		additionalAnnotations["nginx.ingress.kubernetes.io/server-snippet"] = "add_header X-Robots-Tag \"noindex, nofollow\";\n"
	}

	// if idling request verification is in the `.lagoon.yml` and true, add the annotation. this supports production and development environment types
	// in the event that production environments support idling properly that option could be available to then
	// idle standby environments or production environments generally in opensource lagoon
	if route.RequestVerification != nil && *route.RequestVerification {
		// @TODO: this will eventually be changed to a `lagoon.sh` instead of `amazee.io` namespaced annotation in the future once
		// aergia is fully integrated into the uselagoon namespace
		additionalAnnotations["idling.amazee.io/disable-request-verification"] = "true"
	} else {
		// otherwise force false
		additionalAnnotations["idling.amazee.io/disable-request-verification"] = "false"
	}

	// check if a user has defined hsts configuration
	if route.HSTSEnabled != nil && *route.HSTSEnabled {
		hstsHeader := fmt.Sprintf("more_set_headers \"Strict-Transport-Security: max-age=%d", route.HSTSMaxAge)
		if route.HSTSIncludeSubdomains != nil && *route.HSTSIncludeSubdomains {
			hstsHeader = fmt.Sprintf("%s%s", hstsHeader, ";includeSubDomains")
		}
		if route.HSTSPreload != nil && *route.HSTSPreload {
			hstsHeader = fmt.Sprintf("%s%s", hstsHeader, ";preload")
		}
		hstsHeader = fmt.Sprintf("%s\"", hstsHeader)
		// if someone has already set a configuration-snippet annotation, then add the hsts header
		// to the top of the existing annotation before it is added to the ingress object
		if value, ok := route.Annotations["nginx.ingress.kubernetes.io/configuration-snippet"]; ok {
			route.Annotations["nginx.ingress.kubernetes.io/configuration-snippet"] = fmt.Sprintf(
				"%s;\n%s",
				hstsHeader,
				value,
			)
		} else {
			// otherwise create a new one in the additional annotations
			additionalAnnotations["nginx.ingress.kubernetes.io/configuration-snippet"] = fmt.Sprintf(
				"%s;\n",
				hstsHeader,
			)
		}
	}

	// add ingressclass support to ingress template generation
	if route.IngressClass != "" {
		ingress.Spec.IngressClassName = &route.IngressClass
		// add the certmanager ingressclass annotation
		additionalAnnotations["acme.cert-manager.io/http01-ingress-class"] = route.IngressClass
	}

	// add any additional labels
	for key, value := range additionalLabels {
		ingress.ObjectMeta.Labels[key] = value
	}
	// add any additional annotations
	for key, value := range additionalAnnotations {
		ingress.ObjectMeta.Annotations[key] = value
	}
	// add any annotations that the route had to overwrite any previous annotations
	for key, value := range route.Annotations {
		ingress.ObjectMeta.Annotations[key] = value
	}
	// add any labels that the route had to overwrite any previous labels
	for key, value := range route.Labels {
		ingress.ObjectMeta.Labels[key] = value
	}
	// validate any annotations
	if err := apivalidation.ValidateAnnotations(ingress.ObjectMeta.Annotations, nil); err != nil {
		if len(err) != 0 {
			return nil, fmt.Errorf("the annotations for %s are not valid: %v", route.Domain, err)
		}
	}
	// validate any labels
	if err := metavalidation.ValidateLabels(ingress.ObjectMeta.Labels, nil); err != nil {
		if len(err) != 0 {
			return nil, fmt.Errorf("the labels for %s are not valid: %v", route.Domain, err)
		}
	}

	// set up the secretname for tls
	if route.Autogenerated {
		// autogenerated use the service name
		ingress.Spec.TLS = []networkv1.IngressTLS{
			{
				SecretName: fmt.Sprintf("%s-tls", route.LagoonService),
			},
		}
	} else {
		// everything else uses the truncated
		ingress.Spec.TLS = []networkv1.IngressTLS{
			{
				// use the truncated route domain here as we add `-tls`
				// if a domain that is 253 chars long is used this will then exceed
				// the 253 char limit on kubernetes names
				SecretName: fmt.Sprintf("%s-tls", truncatedRouteDomain),
			},
		}
	}

	// autogenerated domains that are too long break when creating the acme challenge k8s resource
	// this injects a shorter domain into the tls spec that is used in the k8s challenge
	// use the compose service name to check this, as this is how Services are populated from the compose generation
	for _, service := range lValues.Services {
		if service.OverrideName == route.LagoonService {
			if service.ShortAutogeneratedRouteDomain != "" && len(route.Domain) > 63 {
				ingress.Spec.TLS[0].Hosts = append(ingress.Spec.TLS[0].Hosts, service.ShortAutogeneratedRouteDomain)
			}
		}
	}
	// add the main domain to the tls spec now
	ingress.Spec.TLS[0].Hosts = append(ingress.Spec.TLS[0].Hosts, route.Domain)

	// default service port is http in all lagoon deployments
	// this should be the port that usually would be accessible via an ingress if the service would normally
	// allow this
	servicePort := networkv1.ServiceBackendPort{
		Name: "http",
	}

	backendService := route.LagoonService
	// check the additional service ports if they are there, and check if the provided route service is in this list of additional ports
	// and use the name in the ingress
	for _, service := range lValues.Services {
		for idx, addPort := range service.AdditionalServicePorts {
			if addPort.ServiceName == route.LagoonService {
				servicePort = services.GenerateServiceBackendPort(addPort)
				backendService = service.OverrideName
			}
			// if this service is for the default named lagoonservice
			if service.OverrideName == route.LagoonService && idx == 0 {
				// and set the portname to the name of the first service in the list
				servicePort = services.GenerateServiceBackendPort(addPort)
			}
		}
	}

	// set up the pathtype prefix for the host rule
	pt := networkv1.PathTypePrefix

	// set up the default path to point to the first backend service as required
	paths := []networkv1.HTTPIngressPath{
		{
			Path:     "/",
			PathType: &pt,
			Backend: networkv1.IngressBackend{
				Service: &networkv1.IngressServiceBackend{
					Name: backendService,
					Port: servicePort,
				},
			},
		},
	}

	// check for any path based routes defined against this ingress
	for _, pr := range route.PathRoutes {
		// default path routes to the http named backend
		pathPort := networkv1.ServiceBackendPort{
			Name: "http",
		}
		backendServiceName := pr.ToService
		// if a port override service name has been provided because 'lagoon.service.usecomposeports' is defined against a service
		// look it up the provided service against the computed additional ports
		// and extract that ports backend name to use
		for _, service := range lValues.Services {
			// if the toService is the default service name, not a port specific override but additionalserviceports is more than 0
			// then this is the "default" service that is being references
			if pr.ToService == service.OverrideName && len(service.AdditionalServicePorts) > 0 {
				// extract the first port from the additional ports to use as the path port
				// as the first port in the list is the "default" port
				pathPort = services.GenerateServiceBackendPort(service.AdditionalServicePorts[0])
			}
			// otherwise if the user has specified a specific 'servicename-port' in their toService
			// look that up instead and serve the backend as requested
			for _, addPort := range service.AdditionalServicePorts {
				if addPort.ServiceName == pr.ToService {
					pathPort = services.GenerateServiceBackendPort(addPort)
					backendServiceName = addPort.ServiceOverrideName
				}
			}
		}
		// append the ingress paths with the computed details
		paths = append(paths, networkv1.HTTPIngressPath{
			Path:     pr.Path,
			PathType: &pt,
			Backend: networkv1.IngressBackend{
				Service: &networkv1.IngressServiceBackend{
					Name: backendServiceName,
					Port: pathPort,
				},
			},
		})
	}
	// add the main domain as the first rule in the spec
	ingress.Spec.Rules = []networkv1.IngressRule{
		{
			Host: route.Domain,
			IngressRuleValue: networkv1.IngressRuleValue{
				HTTP: &networkv1.HTTPIngressRuleValue{
					Paths: paths,
				},
			},
		},
	}
	// check if any alternative names were provided and add them to the spec
	for _, alternativeName := range route.AlternativeNames {
		ingress.Spec.TLS[0].Hosts = append(ingress.Spec.TLS[0].Hosts, alternativeName)
		altName := networkv1.IngressRule{
			Host: alternativeName,
			IngressRuleValue: networkv1.IngressRuleValue{
				HTTP: &networkv1.HTTPIngressRuleValue{
					Paths: []networkv1.HTTPIngressPath{
						{
							Path:     "/",
							PathType: &pt,
							Backend: networkv1.IngressBackend{
								Service: &networkv1.IngressServiceBackend{
									Name: backendService,
									Port: servicePort,
								},
							},
						},
					},
				},
			},
		}
		ingress.Spec.Rules = append(ingress.Spec.Rules, altName)
	}

	// @TODO: we should review this in the future when we stop doing `kubectl apply` in the builds :)
	// marshal the resulting ingress
	ingressBytes, err := yaml.Marshal(ingress)
	if err != nil {
		return nil, err
	}
	// add the seperator to the template so that it can be `kubectl apply` in bulk as part
	// of the current build process
	separator := []byte("---\n")
	result := append(separator[:], ingressBytes[:]...)
	return result, nil
}
