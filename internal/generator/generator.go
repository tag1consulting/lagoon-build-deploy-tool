package generator

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/uselagoon/build-deploy-tool/internal/helpers"
	"github.com/uselagoon/build-deploy-tool/internal/lagoon"
	"sigs.k8s.io/yaml"
)

type Generator struct {
	LagoonYAML                 *lagoon.YAML
	BuildValues                *BuildValues
	LagoonEnvironmentVariables *[]lagoon.EnvironmentVariable
	ActiveEnvironment          *bool
	StandbyEnvironment         *bool
	AutogeneratedRoutes        *lagoon.RoutesV2
	MainRoutes                 *lagoon.RoutesV2
	ActiveStandbyRoutes        *lagoon.RoutesV2
}

func NewGenerator(
	lagoonYml,
	projectVariables,
	environmentVariables,
	projectName,
	environmentName,
	environmentType,
	activeEnvironment,
	standbyEnvironment,
	buildType,
	branch,
	prNumber,
	prTitle,
	prHeadBranch,
	prBaseBranch,
	lagoonVersion,
	defaultBackupSchedule,
	hourlyDefaultBackupRetention,
	dailyDefaultBackupRetention,
	weeklyDefaultBackupRetention,
	monthlyDefaultBackupRetention,
	monitoringContact,
	monitoringStatusPageID,
	fastlyCacheNoCahce,
	fastlyAPISecretPrefix,
	fastlyServiceID string,
	ignoreNonStringKeyErrors, ignoreMissingEnvFiles, debug bool,
) (*Generator, error) {

	// create some initial variables to be passed through the generators
	buildValues := BuildValues{}
	lYAML := &lagoon.YAML{}
	lagoonEnvVars := []lagoon.EnvironmentVariable{}
	autogenRoutes := &lagoon.RoutesV2{}
	mainRoutes := &lagoon.RoutesV2{}
	activeStandbyRoutes := &lagoon.RoutesV2{}

	// environment variables will override what is provided by flags
	// the following variables have been identified as used by custom-ingress objects
	// these are available within a lagoon build as standard
	monitoringContact = helpers.GetEnv("MONITORING_ALERTCONTACT", monitoringContact, debug)
	monitoringStatusPageID = helpers.GetEnv("MONITORING_STATUSPAGEID", monitoringStatusPageID, debug)
	projectName = helpers.GetEnv("PROJECT", projectName, debug)
	environmentName = helpers.GetEnv("ENVIRONMENT", environmentName, debug)
	branch = helpers.GetEnv("BRANCH", branch, debug)
	prNumber = helpers.GetEnv("PR_NUMBER", prNumber, debug)
	prTitle = helpers.GetEnv("PR_NUMBER", prTitle, debug)
	prHeadBranch = helpers.GetEnv("PR_HEAD_BRANCH", prHeadBranch, debug)
	prBaseBranch = helpers.GetEnv("PR_BASE_BRANCH", prBaseBranch, debug)
	environmentType = helpers.GetEnv("ENVIRONMENT_TYPE", environmentType, debug)
	buildType = helpers.GetEnv("BUILD_TYPE", buildType, debug)
	activeEnvironment = helpers.GetEnv("ACTIVE_ENVIRONMENT", activeEnvironment, debug)
	standbyEnvironment = helpers.GetEnv("STANDBY_ENVIRONMENT", standbyEnvironment, debug)
	fastlyCacheNoCahce = helpers.GetEnv("LAGOON_FASTLY_NOCACHE_SERVICE_ID", fastlyCacheNoCahce, debug)
	fastlyServiceID = helpers.GetEnv("ROUTE_FASTLY_SERVICE_ID", fastlyServiceID, debug)
	fastlyAPISecretPrefix = helpers.GetEnv("ROUTE_FASTLY_SERVICE_ID", fastlyAPISecretPrefix, debug)
	lagoonVersion = helpers.GetEnv("LAGOON_VERSION", lagoonVersion, debug)

	// the following variables are used for backup and schedule configurations
	defaultBackupSchedule = helpers.GetEnv("DEFAULT_BACKUP_SCHEDULE", defaultBackupSchedule, debug)
	hourlyDefaultBackupRetention = helpers.GetEnv("HOURLY_BACKUP_DEFAULT_RETENTION", hourlyDefaultBackupRetention, debug)
	dailyDefaultBackupRetention = helpers.GetEnv("DAILY_BACKUP_DEFAULT_RETENTION", dailyDefaultBackupRetention, debug)
	weeklyDefaultBackupRetention = helpers.GetEnv("WEEKLY_BACKUP_DEFAULT_RETENTION", weeklyDefaultBackupRetention, debug)
	monthlyDefaultBackupRetention = helpers.GetEnv("MONTHLY_BACKUP_DEFAULT_RETENTION", monthlyDefaultBackupRetention, debug)

	// read the .lagoon.yml file
	lPolysite := make(map[string]interface{})
	if err := lagoon.UnmarshalLagoonYAML(lagoonYml, lYAML, &lPolysite); err != nil {
		return nil, fmt.Errorf("couldn't read file %v: %v", lagoonYml, err)
	}

	// if this is a polysite, then unmarshal the polysite data into a normal lagoon environments yaml
	// this is done so that all other generators only need to know how to interact with one type of environment
	if _, ok := lPolysite[projectName]; ok {
		s, _ := yaml.Marshal(lPolysite[projectName])
		_ = yaml.Unmarshal(s, &lYAML)
	}

	// start saving values into the build values variable
	buildValues.Project = projectName
	buildValues.Environment = environmentName
	buildValues.EnvironmentType = environmentType
	buildValues.BuildType = buildType
	buildValues.LagoonVersion = lagoonVersion
	buildValues.ActiveEnvironment = activeEnvironment
	buildValues.StandbyEnvironment = standbyEnvironment
	buildValues.FastlyCacheNoCache = fastlyCacheNoCahce
	buildValues.FastlyAPISecretPrefix = fastlyAPISecretPrefix
	switch buildType {
	case "branch", "promote":
		buildValues.Branch = branch
	case "pullrequest":
		buildValues.PRNumber = prNumber
		buildValues.PRTitle = prTitle
		buildValues.PRHeadBranch = prHeadBranch
		buildValues.PRBaseBranch = prBaseBranch
	}

	// break out of the generator if these requirements are missing
	if projectName == "" || environmentName == "" || environmentType == "" || buildType == "" {
		return nil, fmt.Errorf("Missing arguments: project-name, environment-name, environment-type, or build-type not defined")
	}
	switch buildType {
	case "branch", "promote":
		if branch == "" {
			return nil, fmt.Errorf("Missing arguments: branch not defined")
		}
	case "pullrequest":
		if prNumber == "" || prHeadBranch == "" || prBaseBranch == "" {
			return nil, fmt.Errorf("Missing arguments: pullrequest-number, pullrequest-head-branch, or pullrequest-base-branch not defined")
		}
	}

	// get the dbaas operator http endpoint or fall back to the default
	buildValues.DBaaSOperatorEndpoint = helpers.GetEnv("DBAAS_OPERATOR_HTTP", "dbaas.lagoon.svc:5000", debug)

	// get the project and environment variables
	projectVariables = helpers.GetEnv("LAGOON_PROJECT_VARIABLES", projectVariables, debug)
	environmentVariables = helpers.GetEnv("LAGOON_ENVIRONMENT_VARIABLES", environmentVariables, debug)

	// by default, environment routes are not monitored
	buildValues.Monitoring.Enabled = false
	if environmentType == "production" {
		// if this is a production environment, monitoring IS enabled
		buildValues.Monitoring.Enabled = true
		buildValues.Monitoring.AlertContact = monitoringContact
		buildValues.Monitoring.StatusPageID = monitoringStatusPageID
		// check if the environment is active or standby
		if environmentName == activeEnvironment {
			buildValues.IsActiveEnvironment = true
		}
		if environmentName == standbyEnvironment {
			buildValues.IsStandbyEnvironment = true
		}
	}

	// unmarshal and then merge the two so there is only 1 set of variables to iterate over
	projectVars := []lagoon.EnvironmentVariable{}
	envVars := []lagoon.EnvironmentVariable{}
	json.Unmarshal([]byte(projectVariables), &projectVars)
	json.Unmarshal([]byte(environmentVariables), &envVars)
	mergedVariables := lagoon.MergeVariables(projectVars, envVars)
	// collect a bunch of the default LAGOON_X based build variables that are injected into `lagoon-env` and make them available
	configVars := collectBuildVariables(buildValues)
	// add the calculated build runtime variables into the existing variable slice
	// this will later be used to add `runtime|global` scope into the `lagoon-env` configmap
	lagoonEnvVars = lagoon.MergeVariables(mergedVariables, configVars)

	// get any variables from the API here
	lagoonServiceTypes, _ := lagoon.GetLagoonVariable("LAGOON_SERVICE_TYPES", nil, lagoonEnvVars)
	buildValues.ServiceTypeOverrides = lagoonServiceTypes

	lagoonDBaaSEnvironmentTypes, _ := lagoon.GetLagoonVariable("LAGOON_DBAAS_ENVIRONMENT_TYPES", nil, lagoonEnvVars)
	buildValues.DBaaSEnvironmentTypeOverrides = lagoonDBaaSEnvironmentTypes

	// @TODO: eventually fail builds if this is not set https://github.com/uselagoon/build-deploy-tool/issues/56
	// lagoonDBaaSFallbackSingle, _ := lagoon.GetLagoonVariable("LAGOON_FEATURE_FLAG_DBAAS_FALLBACK_SINGLE", nil, lagoonEnvVars)
	// buildValues.DBaaSFallbackSingle = helpers.StrToBool(lagoonDBaaSFallbackSingle.Value)

	/* start backups configuration */
	err := generateBackupValues(&buildValues, lYAML, lagoonEnvVars, debug)
	if err != nil {
		return nil, err
	}
	/* end backups configuration */

	/* start compose->service configuration */
	err = generateServicesFromDockerCompose(&buildValues, lYAML, lagoonEnvVars, ignoreNonStringKeyErrors, ignoreMissingEnvFiles, debug)
	if err != nil {
		return nil, err
	}
	/* end compose->service configuration */

	/* start route generation */
	// create all the routes for this environment and store the primary and secondary routes into values
	// populate the autogenRoutes, mainRoutes and activeStandbyRoutes here and load them
	buildValues.Route, buildValues.Routes, buildValues.AutogeneratedRoutes, err = generateRoutes(
		lagoonEnvVars,
		buildValues,
		*lYAML,
		autogenRoutes,
		mainRoutes,
		activeStandbyRoutes,
		debug,
	)
	if err != nil {
		return nil, err
	}
	/* end route generation configuration */

	// finally return the generator values
	return &Generator{
		BuildValues:                &buildValues,
		LagoonYAML:                 lYAML,
		LagoonEnvironmentVariables: &lagoonEnvVars,
		ActiveEnvironment:          &buildValues.IsActiveEnvironment,
		StandbyEnvironment:         &buildValues.IsStandbyEnvironment,
		AutogeneratedRoutes:        autogenRoutes,
		MainRoutes:                 mainRoutes,
		ActiveStandbyRoutes:        activeStandbyRoutes,
	}, nil
}

// this creates a bunch of standard environment variables that are injected into the `lagoon-env` configmap normally
func collectBuildVariables(buildValues BuildValues) []lagoon.EnvironmentVariable {
	vars := []lagoon.EnvironmentVariable{}
	vars = append(vars, lagoon.EnvironmentVariable{Name: "LAGOON_PROJECT", Value: buildValues.Project, Scope: "runtime"})
	vars = append(vars, lagoon.EnvironmentVariable{Name: "LAGOON_ENVIRONMENT", Value: buildValues.Environment, Scope: "runtime"})
	vars = append(vars, lagoon.EnvironmentVariable{Name: "LAGOON_ENVIRONMENT_TYPE", Value: buildValues.EnvironmentType, Scope: "runtime"})
	vars = append(vars, lagoon.EnvironmentVariable{Name: "LAGOON_GIT_SHA", Value: buildValues.GitSha, Scope: "runtime"})
	vars = append(vars, lagoon.EnvironmentVariable{Name: "LAGOON_KUBERNETES", Value: buildValues.Kubernetes, Scope: "runtime"})
	vars = append(vars, lagoon.EnvironmentVariable{Name: "LAGOON_GIT_SAFE_BRANCH", Value: buildValues.Environment, Scope: "runtime"}) //deprecated??? (https://github.com/uselagoon/lagoon/blob/1053965321495213591f4c9110f90a9d9dcfc946/images/kubectl-build-deploy-dind/build-deploy-docker-compose.sh#L748)
	if buildValues.BuildType == "branch" {
		vars = append(vars, lagoon.EnvironmentVariable{Name: "LAGOON_GIT_BRANCH", Value: buildValues.Branch, Scope: "runtime"})
	}
	if buildValues.BuildType == "pullrequest" {
		vars = append(vars, lagoon.EnvironmentVariable{Name: "LAGOON_PR_HEAD_BRANCH", Value: buildValues.PRHeadBranch, Scope: "runtime"})
		vars = append(vars, lagoon.EnvironmentVariable{Name: "LAGOON_PR_BASE_BRANCH", Value: buildValues.PRBaseBranch, Scope: "runtime"})
		vars = append(vars, lagoon.EnvironmentVariable{Name: "LAGOON_PR_TITLE", Value: buildValues.PRTitle, Scope: "runtime"})
		vars = append(vars, lagoon.EnvironmentVariable{Name: "LAGOON_PR_NUMBER", Value: buildValues.PRNumber, Scope: "runtime"})
	}
	if buildValues.ActiveEnvironment != "" {
		vars = append(vars, lagoon.EnvironmentVariable{Name: "LAGOON_ACTIVE_ENVIRONMENT", Value: buildValues.ActiveEnvironment, Scope: "runtime"})
	}
	if buildValues.StandbyEnvironment != "" {
		vars = append(vars, lagoon.EnvironmentVariable{Name: "LAGOON_STANDBY_ENVIRONMENT", Value: buildValues.StandbyEnvironment, Scope: "runtime"})
	}
	vars = append(vars, lagoon.EnvironmentVariable{Name: "LAGOON_ROUTE", Value: buildValues.Route, Scope: "runtime"})
	vars = append(vars, lagoon.EnvironmentVariable{Name: "LAGOON_ROUTES", Value: strings.Join(buildValues.Routes, ","), Scope: "runtime"})
	vars = append(vars, lagoon.EnvironmentVariable{Name: "LAGOON_AUTOGENERATED_ROUTES", Value: strings.Join(buildValues.AutogeneratedRoutes, ","), Scope: "runtime"})
	return vars
}
