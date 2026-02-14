package router

import (
	"github.com/openshift/router/test/e2e/testdata"
	"context"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	exutil "github.com/openshift/openshift-tests-private/test/extended/util"
	clusterinfra "github.com/openshift/openshift-tests-private/test/extended/util/clusterinfra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	e2e "k8s.io/kubernetes/test/e2e/framework"
	e2eoutput "k8s.io/kubernetes/test/e2e/framework/pod/output"
)

type ingressControllerDescription struct {
	name        string
	namespace   string
	defaultCert string
	domain      string
	shard       string
	replicas    int
	template    string
}

type ingctrlHostPortDescription struct {
	name        string
	namespace   string
	defaultCert string
	domain      string
	httpport    int
	httpsport   int
	statsport   int
	replicas    int
	template    string
}

type ipfailoverDescription struct {
	name        string
	namespace   string
	image       string
	vip         string
	HAInterface string
	template    string
}

type routeDescription struct {
	name      string
	namespace string
	domain    string
	subDomain string
	template  string
}

type ingressDescription struct {
	name        string
	namespace   string
	domain      string
	serviceName string
	template    string
}

type webServerDeployDescription struct {
	deployName      string
	svcSecureName   string
	svcUnsecureName string
	template        string
	namespace       string
}

type gatewayDescription struct {
	name      string
	namespace string
	hostname  string
	template  string
}

type httpRouteDescription struct {
	name      string
	namespace string
	gwName    string
	hostname  string
	template  string
}

func (ingctrl *ingressControllerDescription) create(oc *exutil.CLI) {
	availableWorkerNode, _ := exactNodeDetails(oc)
	if availableWorkerNode < 1 {
		g.Skip("Skipping as there is no enough worker nodes")
	}
	err := createResourceFromTemplate(oc, "--ignore-unknown-parameters=true", "-f", ingctrl.template, "-p", "NAME="+ingctrl.name, "NAMESPACE="+ingctrl.namespace, "DOMAIN="+ingctrl.domain, "SHARD="+ingctrl.shard)
	o.Expect(err).NotTo(o.HaveOccurred())
}

func (ingctrl *ingressControllerDescription) delete(oc *exutil.CLI) error {
	return oc.AsAdmin().WithoutNamespace().Run("delete").Args("--ignore-not-found", "-n", ingctrl.namespace, "ingresscontroller", ingctrl.name).Execute()
}

// Function to create hostnetwork type ingresscontroller with custom http/https/stat ports
func (ingctrl *ingctrlHostPortDescription) create(oc *exutil.CLI) {
	err := createResourceFromTemplate(oc, "--ignore-unknown-parameters=true", "-f", ingctrl.template, "-p", "NAME="+ingctrl.name, "NAMESPACE="+ingctrl.namespace, "DOMAIN="+ingctrl.domain, "HTTPPORT="+strconv.Itoa(ingctrl.httpport), "HTTPSPORT="+strconv.Itoa(ingctrl.httpsport), "STATSPORT="+strconv.Itoa(ingctrl.statsport))
	o.Expect(err).NotTo(o.HaveOccurred())
}

// Function to delete hostnetwork type ingresscontroller
func (ingctrl *ingctrlHostPortDescription) delete(oc *exutil.CLI) error {
	return oc.AsAdmin().WithoutNamespace().Run("delete").Args("--ignore-not-found", "-n", ingctrl.namespace, "ingresscontroller", ingctrl.name).Execute()
}

// create route object from template.
func (route *routeDescription) create(oc *exutil.CLI) {
	err := createResourceToNsFromTemplate(oc, route.namespace, "--ignore-unknown-parameters=true", "-f", route.template, "-p", "SUBDOMAIN_NAME="+route.subDomain, "NAMESPACE="+route.namespace, "DOMAIN="+route.domain)
	o.Expect(err).NotTo(o.HaveOccurred())
}

// create ingress object from template.
func (ing *ingressDescription) create(oc *exutil.CLI) {
	err := createResourceToNsFromTemplate(oc, ing.namespace, "--ignore-unknown-parameters=true", "-f", ing.template, "-p", "NAME="+ing.name, "NAMESPACE="+ing.namespace, "DOMAIN="+ing.domain, "SERVICE_NAME="+ing.serviceName)
	o.Expect(err).NotTo(o.HaveOccurred())
}

// create web server deployment from template
func (websrvdeploy *webServerDeployDescription) create(oc *exutil.CLI) {
	err := createResourceToNsFromTemplate(oc, websrvdeploy.namespace, "--ignore-unknown-parameters=true", "-f", websrvdeploy.template, "-p", "DEPLOY_NAME="+websrvdeploy.deployName, "SVC_SECURE_NAME="+websrvdeploy.svcSecureName, "SVC_UNSECURE_NAME="+websrvdeploy.svcUnsecureName)
	o.Expect(err).NotTo(o.HaveOccurred())
}

func (websrvdeploy *webServerDeployDescription) delete(oc *exutil.CLI) error {
	return oc.AsAdmin().WithoutNamespace().Run("delete").Args("-n", websrvdeploy.namespace, "deployment", websrvdeploy.deployName).Execute()
}

func (gw *gatewayDescription) create(oc *exutil.CLI) {
	err := createResourceToNsFromTemplate(oc, gw.namespace, "--ignore-unknown-parameters=true", "-f", gw.template, "-p", "NAME="+gw.name, "NAMESPACE="+gw.namespace, "HOSTNAME="+gw.hostname)
	o.Expect(err).NotTo(o.HaveOccurred())
}

func (gw *gatewayDescription) delete(oc *exutil.CLI) error {
	return oc.AsAdmin().WithoutNamespace().Run("delete").Args("-n", gw.namespace, "gateway", gw.name).Execute()
}

func (httpRoute *httpRouteDescription) userCreate(oc *exutil.CLI) {
	output, err := createUserResourceToNsFromTemplate(oc, httpRoute.namespace, "--ignore-unknown-parameters=true", "-f", httpRoute.template, "-p", "NAME="+httpRoute.name, "NAMESPACE="+httpRoute.namespace, "GWNAME="+httpRoute.gwName, "HOSTNAME="+httpRoute.hostname)
	o.Expect(err).NotTo(o.HaveOccurred())
	o.Expect(output).To(o.ContainSubstring(`httproute.gateway.networking.k8s.io/` + httpRoute.name + ` created`))
}

func createResourceFromTemplate(oc *exutil.CLI, parameters ...string) error {
	jsonCfg := parseToJSON(oc, parameters)
	return oc.AsAdmin().WithoutNamespace().Run("create").Args("-f", jsonCfg).Execute()
}

func createResourceToNsFromTemplate(oc *exutil.CLI, ns string, parameters ...string) error {
	jsonCfg := parseToJSON(oc, parameters)
	return oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", ns, "-f", jsonCfg).Execute()
}

func createUserResourceToNsFromTemplate(oc *exutil.CLI, ns string, parameters ...string) (string, error) {
	jsonCfg := parseToJSON(oc, parameters)
	return oc.WithoutNamespace().Run("create").Args("-n", ns, "-f", jsonCfg).Output()
}

func getRandomString() string {
	chars := "abcdefghijklmnopqrstuvwxyz0123456789"
	seed := rand.New(rand.NewSource(time.Now().UnixNano()))
	buffer := make([]byte, 8)
	for index := range buffer {
		buffer[index] = chars[seed.Intn(len(chars))]
	}
	return string(buffer)
}

func getFixedLengthRandomString(length int) string {
	const chars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	letterBytes := []byte(chars)
	result := make([]byte, length)
	for i := range result {
		result[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(result)
}

func getBaseDomain(oc *exutil.CLI) string {
	var basedomain string

	basedomain, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("dns.config/cluster", "-o=jsonpath={.spec.baseDomain}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("the base domain of the cluster: %v", basedomain)
	return basedomain
}

// extractGCPZoneName extracts the zone name from GCP zone ID
// In OpenShift 4.20+, GCP zone IDs are in the format: projects/{projectID}/managedZones/{zoneName}
// In OpenShift 4.19 and earlier, zone IDs are just the zone name
// This function handles both formats and returns just the zone name
func extractGCPZoneName(zoneID string) string {
	// Check if zoneID is in the new full path format
	if strings.Contains(zoneID, "managedZones/") {
		// Extract zone name from the full path format
		parts := strings.Split(zoneID, "managedZones/")
		if len(parts) == 2 {
			zoneName := parts[1]
			e2e.Logf("Extracted GCP zone name '%s' from full path '%s'", zoneName, zoneID)
			return zoneName
		} else {
			e2e.Failf("Cannot find GCP zone name from full path '%s'", zoneID)
		}
	}
	// Return as-is for the old format (just the zone name)
	e2e.Logf("Using GCP zone ID as-is: %s", zoneID)
	return zoneID
}

// to exact available linux worker node count and details
// for debugging: also list non linux or workers with special labels
func exactNodeDetails(oc *exutil.CLI) (int, string) {
	linuxWorkerDetails, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("nodes", "--selector=node-role.kubernetes.io/worker=,kubernetes.io/os=linux").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	nodeCount := int(strings.Count(linuxWorkerDetails, "Ready")) - (int(strings.Count(linuxWorkerDetails, "SchedulingDisabled")) + int(strings.Count(linuxWorkerDetails, "NotReady")))
	e2e.Logf("Linux worker node details are:\n%v", linuxWorkerDetails)
	e2e.Logf("Available linux worker node count is: %v", nodeCount)
	// checking other type workers for debugging
	nonLinuxWorker, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("nodes", "--selector=node-role.kubernetes.io/worker=,kubernetes.io/os!=linux").Output()
	if !strings.Contains(nonLinuxWorker, "No resources found") {
		e2e.Logf("Found non linux worker nodes and details are:\n%v", nonLinuxWorker)
	}
	remoteWorker, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("nodes", "--selector=node.openshift.io/remote-worker").Output()
	if !strings.Contains(remoteWorker, "No resources found") {
		e2e.Logf("Found remote worker nodes and details are:\n%v", remoteWorker)
	}
	outpostWorker, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("nodes", "--selector=topology.ebs.csi.aws.com/outpost-id").Output()
	if !strings.Contains(outpostWorker, "No resources found") {
		e2e.Logf("Found outpost worker nodes and details are:\n%v", outpostWorker)
	}
	localZoneWorker, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("nodes", "--selector=node-role.kubernetes.io/edge").Output()
	if !strings.Contains(localZoneWorker, "No resources found") {
		e2e.Logf("Found local zone worker nodes and details are:\n%v", localZoneWorker)
	}
	return nodeCount, linuxWorkerDetails
}

// parse the yaml file to json.
func parseToJSON(oc *exutil.CLI, parameters []string) string {
	var jsonCfg string
	err := wait.Poll(3*time.Second, 15*time.Second, func() (bool, error) {
		output, err := oc.AsAdmin().Run("process").Args(parameters...).OutputToFile(getRandomString() + "-temp-resource.json")
		if err != nil {
			e2e.Logf("the err:%v, and try next round", err)
			return false, nil
		}
		jsonCfg = output
		return true, nil
	})
	exutil.AssertWaitPollNoErr(err, fmt.Sprintf("fail to process %v", parameters))
	e2e.Logf("the file of resource is %s", jsonCfg)
	return jsonCfg
}

func waitForCustomIngressControllerAvailable(oc *exutil.CLI, icname string) error {
	e2e.Logf("check ingresscontroller if available")
	return wait.Poll(5*time.Second, 3*time.Minute, func() (bool, error) {
		status, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("ingresscontroller", icname, "--namespace=openshift-ingress-operator", `-ojsonpath={.status.conditions[?(@.type=="Available")].status}`).Output()
		e2e.Logf("the status of ingresscontroller is %v", status)
		if err != nil || status == "" {
			e2e.Logf("failed to get ingresscontroller %s: %v, retrying...", icname, err)
			return false, nil
		}
		if strings.Contains(status, "False") {
			e2e.Logf("ingresscontroller %s conditions not available, retrying...", icname)
			return false, nil
		}
		return true, nil
	})
}

func ensureCustomIngressControllerAvailable(oc *exutil.CLI, icName string) {
	ns := "openshift-ingress-operator"
	err := waitForCustomIngressControllerAvailable(oc, icName)
	// print custom ingresscontroller description for debugging purpose if err
	if err != nil {
		output, _ := oc.AsAdmin().WithoutNamespace().Run("describe").Args("-n", ns, "ingresscontroller", icName).Output()
		e2e.Logf("The description of ingresscontroller %v is:\n%v", icName, output)
	}
	exutil.AssertWaitPollNoErr(err, fmt.Sprintf("max time reached but ingresscontroller %v is not available", icName))
}

func ensureRouteIsAdmittedByIngressController(oc *exutil.CLI, ns, routeName, icName string) {
	jsonPath := fmt.Sprintf(`{.status.ingress[?(@.routerName=="%s")].conditions[?(@.type=="Admitted")].status}`, icName)
	waitForOutputEquals(oc, ns, "route/"+routeName, jsonPath, "True")
}

func ensureRouteIsNotAdmittedByIngressController(oc *exutil.CLI, ns, routeName, icName string) {
	jsonPath := fmt.Sprintf(`{.status.ingress[?(@.routerName=="%s")].conditions[?(@.type=="Admitted")].status}`, icName)
	waitForOutputEquals(oc, ns, "route/"+routeName, jsonPath, "False")
}

func getOnePodNameByLabel(oc *exutil.CLI, ns, label string) string {
	podName, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("pods", "-l", label, "-o=jsonpath={.items[0].metadata.name}", "-n", ns).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("the one pod with label %v is %v", label, podName)
	return podName
}

// getOneNewRouterPodFromRollingUpdate immediatly after/during deployment rolling update, don't care the previous pod status
func getOneNewRouterPodFromRollingUpdate(oc *exutil.CLI, icName string) string {
	ns := "openshift-ingress"
	deployName := "deployment/router-" + icName
	rsLabel := ""
	re := regexp.MustCompile(`NewReplicaSet:\s+router-.+-([a-z0-9]+)\s+`)
	waitErr := wait.PollImmediate(3*time.Second, 15*time.Second, func() (bool, error) {
		output, _ := oc.AsAdmin().WithoutNamespace().Run("describe").Args(deployName, "-n", ns).Output()
		hash := re.FindStringSubmatch(output)
		if len(hash) > 1 {
			rsLabel = "pod-template-hash=" + hash[1]
			return true, nil
		}
		return false, nil
	})
	exutil.AssertWaitPollNoErr(waitErr, fmt.Sprintf("reached max time allowed but NewReplicaSet not found"))
	e2e.Logf("the new ReplicaSet labels is %s", rsLabel)
	err := waitForPodWithLabelReady(oc, ns, rsLabel)
	if err != nil {
		output, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("pods", "-n", ns).Output()
		e2e.Logf("All current router pods are:\n%v", output)
	}
	exutil.AssertWaitPollNoErr(err, "the new router pod failed to be ready within allowed time!")
	return getOnePodNameByLabel(oc, ns, rsLabel)
}

func ensureRouterDeployGenerationIs(oc *exutil.CLI, icName, expectGeneration string) {
	ns := "openshift-ingress"
	deployName := "deployment/router-" + icName
	actualGeneration := "0"

	waitErr := wait.PollImmediate(3*time.Second, 30*time.Second, func() (bool, error) {
		actualGeneration, _ = oc.AsAdmin().WithoutNamespace().Run("get").Args(deployName, "-n", ns, "-o=jsonpath={.metadata.generation}").Output()
		e2e.Logf("Get the deployment generation is: %v", actualGeneration)
		if actualGeneration == expectGeneration {
			e2e.Logf("The router deployment generation is updated to %v", actualGeneration)
			return true, nil
		}
		return false, nil
	})
	exutil.AssertWaitPollNoErr(waitErr, fmt.Sprintf("max time reached and the expected deployment generation is %v but got %v", expectGeneration, actualGeneration))
}

func waitForPodWithLabelReady(oc *exutil.CLI, ns, label string) error {
	return wait.Poll(5*time.Second, 3*time.Minute, func() (bool, error) {
		status, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("pod", "-n", ns, "-l", label, `-ojsonpath={.items[*].status.conditions[?(@.type=="Ready")].status}`).Output()
		e2e.Logf("the Ready status of pod is %v", status)
		if err != nil || status == "" {
			e2e.Logf("failed to get pod status: %v, retrying...", err)
			return false, nil
		}
		if strings.Contains(status, "False") {
			e2e.Logf("the pod Ready status not met; wanted True but got %v, retrying...", status)
			return false, nil
		}
		return true, nil
	})
}

func ensurePodWithLabelReady(oc *exutil.CLI, ns, label string) {
	err := waitForPodWithLabelReady(oc, ns, label)
	// print pod status and logs for debugging purpose if err
	if err != nil {
		output, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("pod", "-n", ns, "-l", label).Output()
		e2e.Logf("All pods with label %v are:\n%v", label, output)
		logs, _ := oc.AsAdmin().WithoutNamespace().Run("logs").Args("-n", ns, "-l", label, "--tail=10").Output()
		e2e.Logf("The logs of all labeled pods are:\n%v", logs)
	}
	exutil.AssertWaitPollNoErr(err, fmt.Sprintf("max time reached but the pods with label %v are not ready", label))
}

func waitForPodWithLabelAppear(oc *exutil.CLI, ns, label string) error {
	return wait.Poll(5*time.Second, 3*time.Minute, func() (bool, error) {
		podList, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("pod", "-n", ns, "-l", label).Output()
		e2e.Logf("the pod list is %v", podList)
		// add check for OCPQE-17360: pod list is "No resources found in xxx namespace"
		podFlag := 1
		if strings.Contains(podList, "No resources found") {
			podFlag = 0
		}
		if err != nil || len(podList) < 1 || podFlag == 0 {
			e2e.Logf("failed to get pod: %v, retrying...", err)
			return false, nil
		}
		return true, nil
	})
}

// wait for the named resource is disappeared, e.g. used while router deployment rolled out
func waitForResourceToDisappear(oc *exutil.CLI, ns, rsname string) error {
	return wait.Poll(20*time.Second, 5*time.Minute, func() (bool, error) {
		status, err := oc.AsAdmin().WithoutNamespace().Run("get").Args(rsname, "-n", ns).Output()
		e2e.Logf("check resource %v and got: %v", rsname, status)
		primary := false
		if err != nil {
			if strings.Contains(status, "NotFound") {
				e2e.Logf("the resource is disappeared!")
				primary = true
			} else {
				e2e.Logf("failed to get the resource: %v, retrying...", err)
			}
		} else {
			e2e.Logf("the resource is still there, retrying...")
		}
		return primary, nil
	})
}

// For normal user to create resources in the specified namespace from the file (not template)
func createResourceFromFile(oc *exutil.CLI, ns, file string) {
	err := oc.WithoutNamespace().Run("create").Args("-f", file, "-n", ns).Execute()
	o.Expect(err).NotTo(o.HaveOccurred())
}

// to use createResourceFromFile function to create resources from files like web-server-rc.yaml and web-server-signed-deploy.yaml
func createResourceFromWebServer(oc *exutil.CLI, ns, file, srvrcInfo string) []string {
	createResourceFromFile(oc, ns, file)
	err := waitForPodWithLabelReady(oc, ns, "name="+srvrcInfo)
	exutil.AssertWaitPollNoErr(err, "backend server pod failed to be ready state within allowed time!")
	srvPodList := getPodListByLabel(oc, ns, "name="+srvrcInfo)
	return srvPodList
}

// For admin user to create/delete resources in the specified namespace from the file (not template)
// oper, should be create or delete
func operateResourceFromFile(oc *exutil.CLI, oper, ns, file string) {
	err := oc.AsAdmin().WithoutNamespace().Run(oper).Args("-f", file, "-n", ns).Execute()
	o.Expect(err).NotTo(o.HaveOccurred())
}

// For normal user to patch a resource in the specified namespace
func patchResourceAsUser(oc *exutil.CLI, ns, resource, patch string) {
	err := oc.WithoutNamespace().Run("patch").Args(resource, "-p", patch, "--type=merge", "-n", ns).Execute()
	o.Expect(err).NotTo(o.HaveOccurred())
}

// For Admin to patch a resource in the specified namespace with 'merge' type
func patchResourceAsAdmin(oc *exutil.CLI, ns, resource, patch string) {
	patchResourceAsAdminAnyType(oc, ns, resource, patch, "merge")
}

// For Admin to patch a resource in the specified namespace with any TYPE
// Type can be any like 'merge', 'json' etc
func patchResourceAsAdminAnyType(oc *exutil.CLI, ns, resource, patch, typ string) {
	err := oc.AsAdmin().WithoutNamespace().Run("patch").Args(resource, "-p", patch, "--type="+typ, "-n", ns).Execute()
	o.Expect(err).NotTo(o.HaveOccurred())
}

func patchResourceAsAdminWithErrorOutput(oc *exutil.CLI, ns, resource, patch string) string {
	output, err := oc.AsAdmin().WithoutNamespace().Run("patch").Args(resource, "-p", patch, "--type=merge", "-n", ns).Output()
	o.Expect(err).To(o.HaveOccurred())
	return output
}

// To patch global resources as Admin. Can used for patching resources such as ingresses or CVO
func patchGlobalResourceAsAdmin(oc *exutil.CLI, resource, patch string) {
	patchOut, err := oc.AsAdmin().WithoutNamespace().Run("patch").Args(resource, "--patch="+patch, "--type=json").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("The output from the patch is:- %q ", patchOut)
}

// For Admin to patch a resource in the specified namespace, and then return the output after the patching operation
func patchResourceAsAdminAndGetLog(oc *exutil.CLI, ns, resource, patch string) (string, error) {
	outPut, err := oc.AsAdmin().WithoutNamespace().Run("patch").Args(resource, "-p", patch, "--type=merge", "-n", ns).Output()
	return outPut, err
}

func createRoute(oc *exutil.CLI, ns, routeType, routeName, serviceName string, extraParas []string) {
	if routeType == "http" {
		cmd := []string{"-n", ns, "service", serviceName, "--name=" + routeName}
		cmd = append(cmd, extraParas...)
		_, err := oc.Run("expose").Args(cmd...).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
	} else {
		cmd := []string{"-n", ns, "route", routeType, routeName, "--service=" + serviceName}
		cmd = append(cmd, extraParas...)
		_, err := oc.WithoutNamespace().Run("create").Args(cmd...).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
	}
}

func setAnnotation(oc *exutil.CLI, ns, resource, annotation string) {
	err := oc.Run("annotate").Args("-n", ns, resource, annotation, "--overwrite").Execute()
	o.Expect(err).NotTo(o.HaveOccurred())
}

// this function will set the annotation for the given resource
func setAnnotationAsAdmin(oc *exutil.CLI, ns, resource, annotation string) {
	err := oc.AsAdmin().WithoutNamespace().Run("annotate").Args("-n", ns, resource, annotation, "--overwrite").Execute()
	o.Expect(err).NotTo(o.HaveOccurred())
}

// this function will read the annotation from the given resource
func getAnnotation(oc *exutil.CLI, ns, resource, resourceName string) string {
	findAnnotation, err := oc.AsAdmin().WithoutNamespace().Run("get").Args(
		resource, resourceName, "-n", ns, "-o=jsonpath={.metadata.annotations}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	return findAnnotation
}

func setEnvVariable(oc *exutil.CLI, ns, resource, envstring string) {
	err := oc.AsAdmin().WithoutNamespace().Run("set").Args("env", "-n", ns, resource, envstring).Execute()
	o.Expect(err).NotTo(o.HaveOccurred())
	time.Sleep(10 * time.Second)
}

// Generic function to collect resource values with jsonpath option
func getByJsonPath(oc *exutil.CLI, ns, resource, jsonPath string) string {
	output, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("-n", ns, resource, "-o=jsonpath="+jsonPath).Output()
	if err != nil {
		e2e.Logf("the error is: %v", err.Error())
	}
	e2e.Logf("the output filtered by jsonpath is: %v", output)
	return output
}

// this function get resource using label and filtered by jsonpath
func getByLabelAndJsonPath(oc *exutil.CLI, ns, resource, label, jsonPath string) string {
	output, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("-n", ns, resource, "-l", label, "-ojsonpath="+jsonPath).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("the output filtered by label and jsonpath is: %v", output)
	return output
}

// getNodeNameByPod gets the pod located node's name
func getNodeNameByPod(oc *exutil.CLI, namespace string, podName string) string {
	nodeName, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("pod", podName, "-n", namespace, "-o=jsonpath={.spec.nodeName}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("The nodename for pod %s in namespace %s is %s", podName, namespace, nodeName)
	return nodeName
}

// Collect pod describe command details:
func describePodResource(oc *exutil.CLI, podName, namespace string) string {
	podDescribe, err := oc.AsAdmin().WithoutNamespace().Run("describe").Args("pod", podName, "-n", namespace).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	return podDescribe
}

// for collecting a single pod name for general use.
// usage example: podname := getOneRouterPodNameByIC(oc, "default/labelname")
// note: it might get wrong pod which will be terminated during deployment rolling update
func getOneRouterPodNameByIC(oc *exutil.CLI, icname string) string {
	podName, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("pods", "-l", "ingresscontroller.operator.openshift.io/deployment-ingresscontroller="+icname, "-o=jsonpath={.items[0].metadata.name}", "-n", "openshift-ingress").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("the result of router pod name: %v", podName)
	return podName
}

// For collecting env details with grep from router pod [usage example: readRouterPodEnv(oc, podname, "search string")] .
// NOTE: This requires getOneRouterPodNameByIC function to collect the podname variable first!
func readRouterPodEnv(oc *exutil.CLI, routername, envname string) string {
	ns := "openshift-ingress"
	output := readPodEnv(oc, routername, ns, envname)
	return output
}

// For collecting env details with grep [usage example: readPodEnv(oc, namespace, podname, "search string")]
func readPodEnv(oc *exutil.CLI, routername, ns string, envname string) string {
	cmd := fmt.Sprintf("/usr/bin/env | grep %s", envname)
	output, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", ns, routername, "--", "bash", "-c", cmd).Output()
	if err != nil {
		output = "NotFound"
	}
	e2e.Logf("the matched Env are:\n%v", output)
	return output
}

// to check the route data is present in the haproxy.config
// blockCfgStart is the used to get the bulk config from the getBlockConfig function
// searchList is used to locate the specified route config
func ensureHaproxyBlockConfigContains(oc *exutil.CLI, routerPodName string, blockCfgStart string, searchList []string) string {
	var (
		haproxyCfg string
		j          = 0
	)

	e2e.Logf("Polling and search haproxy config file")
	waitErr := wait.Poll(5*time.Second, 60*time.Second, func() (bool, error) {
		haproxyCfg = getBlockConfig(oc, routerPodName, blockCfgStart)
		for i := j; i < len(searchList); i++ {
			if strings.Contains(haproxyCfg, searchList[i]) {
				e2e.Logf("Found the given string %v in haproxy.config", searchList[i])
				j++
				if j == len(searchList) {
					e2e.Logf("All the given strings are found in haproxy.config")
					return true, nil
				}
			} else {
				e2e.Logf("The given string %v is still not found in haproxy.config, retrying...", searchList[i])
				return false, nil
			}
		}
		return false, nil
	})

	exutil.AssertWaitPollNoErr(waitErr, fmt.Sprintf("Reached max time allowed but the given string was not found in haproxy.config"))
	e2e.Logf("The part of haproxy.config that matching \"%s\" is:\n%v", blockCfgStart, haproxyCfg)
	return haproxyCfg
}

// similar to ensureHaproxyBlockConfigContains function, this function uses regexp searching not string searching
func ensureHaproxyBlockConfigMatchRegexp(oc *exutil.CLI, routerPodName string, blockCfgStart string, searchList []string) string {
	var (
		haproxyCfg string
		j          = 0
	)

	e2e.Logf("Polling and search haproxy config file")
	waitErr := wait.Poll(5*time.Second, 60*time.Second, func() (bool, error) {
		haproxyCfg = getBlockConfig(oc, routerPodName, blockCfgStart)
		for i := j; i < len(searchList); i++ {
			searchInfo := regexp.MustCompile(searchList[i]).FindStringSubmatch(haproxyCfg)
			if len(searchInfo) > 0 {
				e2e.Logf("Found the given string %v in haproxy.config", searchList[i])
				j++
				if j == len(searchList) {
					e2e.Logf("All the given strings are found in haproxy.config")
					return true, nil
				}
			} else {
				e2e.Logf("The given string %v is still not found in haproxy.config, retrying...", searchList[i])
				return false, nil
			}
		}
		return false, nil
	})

	exutil.AssertWaitPollNoErr(waitErr, fmt.Sprintf("Reached max time allowed but expected string was not found in haproxy.config"))
	e2e.Logf("The part of haproxy.config that matching \"%s\" is:\n%v", blockCfgStart, haproxyCfg)
	return haproxyCfg
}

// to check the route data is not present in the haproxy.config
// blockCfgStart is the used to get the bulk config from the getBlockConfig function
// searchList is used to locate the specified route config
func ensureHaproxyBlockConfigNotContains(oc *exutil.CLI, routerPodName string, blockCfgStart string, searchList []string) string {
	var (
		haproxyCfg string
		j          = 0
	)

	e2e.Logf("Polling and search haproxy config file")
	waitErr := wait.Poll(5*time.Second, 30*time.Second, func() (bool, error) {
		haproxyCfg = getBlockConfig(oc, routerPodName, blockCfgStart)
		for i := j; i < len(searchList); i++ {
			if !strings.Contains(haproxyCfg, searchList[i]) {
				e2e.Logf("Could not found the given string %v in haproxy.config as expected", searchList[i])
				j++
				if j == len(searchList) {
					e2e.Logf("Could not found all given strings in haproxy.config as expected")
					return true, nil
				}
			} else {
				e2e.Logf("The given string %v is still present in haproxy.config, retrying...", searchList[i])
				return false, nil
			}
		}
		return false, nil
	})

	exutil.AssertWaitPollNoErr(waitErr, fmt.Sprintf("Reached max time allowed but given string is still present in haproxy.config"))
	e2e.Logf("The part of haproxy.config that matching \"%s\" is:\n%v", blockCfgStart, haproxyCfg)
	return haproxyCfg
}

// similar to ensureHaproxyBlockConfigNotContains function, this function uses regexp searching not string searching
func ensureHaproxyBlockConfigNotMatchRegexp(oc *exutil.CLI, routerPodName string, blockCfgStart string, searchList []string) string {
	var (
		haproxyCfg string
		j          = 0
	)

	e2e.Logf("Polling and search haproxy config file")
	waitErr := wait.Poll(5*time.Second, 30*time.Second, func() (bool, error) {
		haproxyCfg = getBlockConfig(oc, routerPodName, blockCfgStart)
		for i := j; i < len(searchList); i++ {
			searchInfo := regexp.MustCompile(searchList[i]).FindStringSubmatch(haproxyCfg)
			if len(searchInfo) == 0 {
				e2e.Logf("Could not found the given string %v in haproxy.config as expected", searchList[i])
				j++
				if j == len(searchList) {
					e2e.Logf("Could not found all given strings in haproxy.config as expected")
					return true, nil
				}
			} else {
				e2e.Logf("The given string %v is still present in haproxy.config, retrying...", searchList[i])
				return false, nil
			}
		}
		return false, nil
	})

	exutil.AssertWaitPollNoErr(waitErr, fmt.Sprintf("Reached max time allowed but given string is still present in haproxy.config"))
	e2e.Logf("The part of haproxy.config that matching \"%s\" is:\n%v", blockCfgStart, haproxyCfg)
	return haproxyCfg
}

// used to get block content of haproxy.conf, for example, get one route's whole backend's configuration specified by searchString(for exmpale: "be_edge_http:" + ns + ":r1-edg")
func getBlockConfig(oc *exutil.CLI, routerPodName, searchString string) string {
	output, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", routerPodName, "--", "bash", "-c", "cat haproxy.config").Output()
	o.Expect(err).NotTo(o.HaveOccurred(), "get the content of haproxy.config failed")
	result := ""
	flag := 0
	startIndex := 0
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, searchString) {
			result = result + line + "\n"
			flag = 1
			startIndex = len(line) - len(strings.TrimLeft(line, " "))
		} else if flag == 1 {
			lineLen := len(line)
			if lineLen == 0 {
				result = result + "\n"
			} else {
				currentIndex := len(line) - len(strings.TrimLeft(line, " "))
				if currentIndex > startIndex {
					result = result + line + "\n"
				} else {
					flag = 2
				}
			}

		} else if flag == 2 {
			break
		}
	}
	e2e.Logf("The block configuration in haproxy that matching \"%s\" is:\n%v", searchString, result)
	return result
}

// this function is used to get haproxy's version
func getHAProxyVersion(oc *exutil.CLI) string {
	var proxyVersion = "notFound"
	routerpod := getOneRouterPodNameByIC(oc, "default")
	haproxyOutput, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", routerpod, "--", "bash", "-c", "haproxy -v | grep version").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	haproxyRe := regexp.MustCompile("([0-9\\.]+)-([0-9a-z]+)")
	haproxyInfo := haproxyRe.FindStringSubmatch(haproxyOutput)
	if len(haproxyInfo) > 0 {
		proxyVersion = haproxyInfo[0]
	}
	return proxyVersion
}

func getHAProxyRPMVersion(oc *exutil.CLI) string {
	routerpod := getOneRouterPodNameByIC(oc, "default")
	haproxyOutput, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", routerpod, "--", "bash", "-c", "rpm -qa | grep haproxy").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	return haproxyOutput
}

// this function is used to check whether a route pod has the specified certification of a route and returns its details
// ns: the route's namespace; routeName: the route's name
// certType: certs or cacerts
// option: --hasCert(means should have the certification) or --noCert(means shouldn't has the certification)
func checkRouteCertificationInRouterPod(oc *exutil.CLI, ns, routeName, routerpod, certType, option string) string {
	var certCfg, cmd string
	var err error
	certName := fmt.Sprintf("%s:%s.pem", ns, routeName)
	if option == "--noCert" && certType == "cacerts" {
		cmd = "ls /var/lib/haproxy/router/cacerts/"
	} else if option != "--noCert" && certType != "cacerts" {
		cmd = "ls --full-time /var/lib/haproxy/router/certs/" + certName
	} else if option != "--noCert" && certType == "cacerts" {
		cmd = "ls --full-time /var/lib/haproxy/router/cacerts/" + certName
	} else {
		cmd = "ls /var/lib/haproxy/router/certs/"
	}

	waitErr := wait.Poll(3*time.Second, 120*time.Second, func() (bool, error) {
		flag := false
		certCfg, err = oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", routerpod, "--", "bash", "-c", cmd).Output()
		if err != nil {
			// make sure the certification is present
			e2e.Logf("Tried to get the certification configuration, but got error(trying...):\n%v", err)
			return false, nil
		}
		if option == "--hasCert" && strings.Contains(certCfg, certName) {
			flag = true
		}
		if option == "--noCert" && !strings.Contains(certCfg, certName) {
			flag = true
		}
		return flag, nil
	})
	exutil.AssertWaitPollNoErr(waitErr, fmt.Sprintf("reached max time allowed but the certification with seaching option %s not matched", option))
	return certCfg
}

func getImagePullSpecFromPayload(oc *exutil.CLI, image string) string {
	var pullspec string
	baseDir := testdata.FixturePath("router")
	indexTmpPath := filepath.Join(baseDir, getRandomString())
	dockerconfigjsonpath := filepath.Join(indexTmpPath, ".dockerconfigjson")
	defer exec.Command("rm", "-rf", indexTmpPath).Output()
	err := os.MkdirAll(indexTmpPath, 0755)
	o.Expect(err).NotTo(o.HaveOccurred())
	_, err = oc.AsAdmin().WithoutNamespace().Run("extract").Args("secret/pull-secret", "-n", "openshift-config", "--confirm", "--to="+indexTmpPath).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	pullspec, err = oc.AsAdmin().WithoutNamespace().Run("adm").Args("release", "info", "--image-for="+image, "-a", dockerconfigjsonpath).Output()
	if err != nil {
		g.Skip("Skipping as failed to get image pull spec from the payload")
	}
	e2e.Logf("the pull spec of image %v is: %v", image, pullspec)
	return pullspec
}

func (ipf *ipfailoverDescription) create(oc *exutil.CLI, ns string) {
	// create ServiceAccount and add it to related SCC
	_, err := oc.AsAdmin().WithoutNamespace().Run("create").Args("sa", "ipfailover", "-n", ns).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	_, err = oc.AsAdmin().WithoutNamespace().Run("adm").Args("policy", "add-scc-to-user", "privileged", "-z", "ipfailover", "-n", ns).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	// create the ipfailover deployment
	err = createResourceFromTemplate(oc, "--ignore-unknown-parameters=true", "-f", ipf.template, "-p", "NAME="+ipf.name, "NAMESPACE="+ipf.namespace, "IMAGE="+ipf.image, "HAINTERFACE="+ipf.HAInterface)
	o.Expect(err).NotTo(o.HaveOccurred())
}

func ensureLogsContainString(oc *exutil.CLI, ns, label, match string) {
	waitErr := wait.Poll(3*time.Second, 90*time.Second, func() (bool, error) {
		log, err := oc.AsAdmin().WithoutNamespace().Run("logs").Args("-n", ns, "-l", label, "--tail=20").Output()
		// for debugging only
		// e2e.Logf("the logs of labeled pods are: %v", log)
		if err != nil || log == "" {
			e2e.Logf("Failed to get logs: %v, retrying...", err)
			return false, nil
		}
		if !strings.Contains(log, match) {
			e2e.Logf("Cannot find the expected string in the logs, retrying...")
			return false, nil
		}
		e2e.Logf("Find the expected string '%v' in the logs.", match)
		return true, nil
	})
	exutil.AssertWaitPollNoErr(waitErr, fmt.Sprintf("Reached max time allowed but cannot find the expected string '%v' in the logs.", match))
}

// This function will identify the master and backup pod of the ipfailover pods
func ensureIpfailoverMasterBackup(oc *exutil.CLI, ns string, podList []string) (string, string) {
	var masterPod, backupPod string
	// The sleep is given for the election process to finish
	time.Sleep(10 * time.Second)
	waitErr := wait.Poll(3*time.Second, 90*time.Second, func() (bool, error) {
		podLogs1, err1 := exutil.GetSpecificPodLogs(oc, ns, "", podList[0], "Entering")
		o.Expect(err1).NotTo(o.HaveOccurred())
		logList1 := strings.Split((strings.TrimSpace(podLogs1)), "\n")
		e2e.Logf("The first pod log's failover status:- %v", podLogs1)
		podLogs2, err2 := exutil.GetSpecificPodLogs(oc, ns, "", podList[1], "Entering")
		o.Expect(err2).NotTo(o.HaveOccurred())
		logList2 := strings.Split((strings.TrimSpace(podLogs2)), "\n")
		e2e.Logf("The second pod log's failover status:- %v", podLogs2)

		// Checking whether the first pod is failover state master and second pod backup
		if strings.Contains(logList1[len(logList1)-1], "(ipfailover_VIP_1) Entering MASTER STATE") {
			if strings.Contains(logList2[len(logList2)-1], "(ipfailover_VIP_1) Entering BACKUP STATE") {
				masterPod = podList[0]
				backupPod = podList[1]
				return true, nil
			}
			// Checking whether the second pod is failover state master and first pod backup
		} else if strings.Contains(logList1[len(logList1)-1], "(ipfailover_VIP_1) Entering BACKUP STATE") {
			if strings.Contains(logList2[len(logList2)-1], "(ipfailover_VIP_1) Entering MASTER STATE") {
				masterPod = podList[1]
				backupPod = podList[0]
				return true, nil
			}
		}
		e2e.Logf("The ipfailover seems not yet converged, retrying again...")
		return false, nil
	})
	exutil.AssertWaitPollNoErr(waitErr, fmt.Sprintf("Reached max time allowed but IPfailover seems not working as expected."))
	e2e.Logf("The Master pod is %v and Backup pod is %v", masterPod, backupPod)
	return masterPod, backupPod
}

// For collecting information from router pod [usage example: readRouterPodData(oc, podname, executeCmd, "search string")] .
// NOTE: This requires getOneRouterPodNameByIC function to collect the podname variable first!
func readRouterPodData(oc *exutil.CLI, routername, executeCmd string, searchString string) string {
	output := readPodData(oc, routername, "openshift-ingress", executeCmd, searchString)
	return output
}

func createConfigMapFromFile(oc *exutil.CLI, ns, name, cmFile string) {
	_, err := oc.AsAdmin().WithoutNamespace().Run("create").Args("configmap", name, "--from-file="+cmFile, "-n", ns).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
}

func deleteConfigMap(oc *exutil.CLI, ns, name string) {
	_, err := oc.AsAdmin().WithoutNamespace().Run("delete").Args("configmap", name, "-n", ns).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
}

// check if a configmap is created in specific namespace [usage: checkConfigMap(oc, namesapce, configmapName)]
func checkConfigMap(oc *exutil.CLI, ns, configmapName string) error {
	return wait.Poll(5*time.Second, 3*time.Minute, func() (bool, error) {
		searchOutput, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("cm", "-n", ns).Output()
		if err != nil {
			e2e.Logf("failed to get configmap: %v", err)
			return false, nil
		}
		if o.Expect(searchOutput).To(o.ContainSubstring(configmapName)) {
			e2e.Logf("configmap %v found", configmapName)
			return true, nil
		}
		return false, nil
	})
}

// To Collect ingresscontroller domain name
func getIngressctlDomain(oc *exutil.CLI, icname string) string {
	var ingressctldomain string
	ingressctldomain, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("ingresscontroller", icname, "--namespace=openshift-ingress-operator", "-o=jsonpath={.status.domain}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("the domain for the ingresscontroller is : %v", ingressctldomain)
	return ingressctldomain
}

// this function helps to get the ipv4 address of the given pod
func getPodv4Address(oc *exutil.CLI, podName, namespace string) string {
	podIPv4, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("pod", podName, "-n", namespace, "-o=jsonpath={.status.podIP}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("IP of the %s pod in namespace %s is %q ", podName, namespace, podIPv4)
	return podIPv4
}

// this function will replace the octate of the ipaddress with the given value
func replaceIPOctet(ipaddress []string, octet int, octetValue string) string {
	ipv4address := ipaddress[0]
	if strings.Count(ipaddress[0], ":") >= 2 {
		ipv4address = ipaddress[1]
	}
	ipList := strings.Split(ipv4address, ".")
	ipList[octet] = octetValue
	vip := strings.Join(ipList, ".")
	e2e.Logf("The modified ipaddress is %s ", vip)
	return vip
}

// this function is to obtain the pod name based on the particular label
func getPodListByLabel(oc *exutil.CLI, namespace string, label string) []string {
	var podList []string
	podNameAll, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("-n", namespace, "pod", "-l", label, "-ojsonpath={.items..metadata.name}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	podList = strings.Split(podNameAll, " ")
	e2e.Logf("The pod list is %v", podList)
	return podList
}

func getDNSPodName(oc *exutil.CLI) string {
	ns := "openshift-dns"
	podName, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("-n", ns, "pods", "-l", "dns.operator.openshift.io/daemonset-dns=default", "-o=jsonpath={.items[0].metadata.name}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("The DNS pod name is: %v", podName)
	return podName
}

// to read the Corefile content in DNS pod
// searchString is to locate the specified section since Corefile might has multiple zones
// that containing same config strings
// grepOptions can specify the lines of the context, e.g. "-A20" or "-C10"
func readDNSCorefile(oc *exutil.CLI, dnsPodName, searchString, grepOption string) string {
	ns := "openshift-dns"
	cmd := fmt.Sprintf("grep \"%s\" /etc/coredns/Corefile %s", searchString, grepOption)
	output, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", ns, dnsPodName, "--", "bash", "-c", cmd).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("the part of Corefile that matching \"%s\" is: %v", searchString, output)
	return output
}

// coredns introduced reload plugin to update the Corefile without receating dns-default pod
// similar to readHaproxyConfig(), use wait.Poll to wait the searchString2 to be updated.
// searchString1 can locate the specified zone section since Corefile might has multiple zones
// grepOptions can specify the lines of the context, e.g. "-A20" or "-C10"
// searchString2 is the config to be checked, it might exist in multiple zones so searchString1 is required
func pollReadDnsCorefile(oc *exutil.CLI, dnsPodName, searchString1, grepOption, searchString2 string) string {
	e2e.Logf("Polling and search dns Corefile")
	ns := "openshift-dns"
	cmd1 := fmt.Sprintf("grep \"%s\" /etc/coredns/Corefile %s | grep \"%s\"", searchString1, grepOption, searchString2)
	cmd2 := fmt.Sprintf("grep \"%s\" /etc/coredns/Corefile %s", searchString1, grepOption)

	waitErr := wait.PollImmediate(5*time.Second, 120*time.Second, func() (bool, error) {
		// trigger an immediately refresh configmap by updating pod's annotations
		hackAnnotatePod(oc, ns, dnsPodName)
		_, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", ns, dnsPodName, "--", "bash", "-c", cmd1).Output()
		if err != nil {
			e2e.Logf("string not found, wait and try again...")
			return false, nil
		}
		return true, nil
	})
	// print all dns pods and one Corefile for debugging (normally the content is less than 20 lines)
	if waitErr != nil {
		output, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("pods", "-n", ns, "-l", "dns.operator.openshift.io/daemonset-dns=default").Output()
		e2e.Logf("All current dns pods are:\n%v", output)
		output, _ = oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", ns, dnsPodName, "--", "bash", "-c", "cat /etc/coredns/Corefile").Output()
		e2e.Logf("The existing Corefile is: %v", output)
	}
	exutil.AssertWaitPollNoErr(waitErr, fmt.Sprintf("reached max time allowed but Corefile is not updated"))
	output, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", ns, dnsPodName, "--", "bash", "-c", cmd2).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("the part of Corefile that matching \"%s\" is: %v", searchString1, output)
	return output
}

// to trigger the configmap refresh immediately
// see https://kubernetes.io/docs/tasks/configure-pod-container/configure-pod-configmap/#mounted-configmaps-are-updated-automatically
func hackAnnotatePod(oc *exutil.CLI, ns, podName string) {
	hackAnnotation := "ne-testing-hack=" + getRandomString()
	oc.AsAdmin().WithoutNamespace().Run("annotate").Args("pod", podName, "-n", ns, hackAnnotation, "--overwrite").Execute()
}

// this function get all cluster's operators
func getClusterOperators(oc *exutil.CLI) []string {
	outputOps, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("clusteroperators", "-o=jsonpath={.items[*].metadata.name}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	opList := strings.Split(outputOps, " ")
	return opList
}

// wait for "Progressing" is True
func ensureClusterOperatorProgress(oc *exutil.CLI, coName string) {
	e2e.Logf("waiting for CO %v to start rolling update......", coName)
	jsonPath := "-o=jsonpath={.status.conditions[?(@.type==\"Progressing\")].status}"
	waitErr := wait.PollImmediate(6*time.Second, 180*time.Second, func() (bool, error) {
		status, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("co/"+coName, jsonPath).Output()
		primary := false
		if strings.Compare(status, "True") == 0 {
			e2e.Logf("Progressing status is True.")
			primary = true
		} else {
			e2e.Logf("Progressing status is not True, wait and try again...")
		}
		return primary, nil
	})
	exutil.AssertWaitPollNoErr(waitErr, fmt.Sprintf(
		"reached max time allowed but CO %v didn't goto Progressing status.", coName))
}

// wait for the cluster operator back to normal status ("True False False")
// wait until get the specified number of successive normal status, which is defined by healthyThreshold and totalWaitTime
// healthyThreshold: max rounds for checking an CO,  int type,and no less than 1
// totalWaitTime: total checking time, time.Durationshould type, and no less than 1
func ensureClusterOperatorNormal(oc *exutil.CLI, coName string, healthyThreshold int, totalWaitTime time.Duration) {
	count := 0
	printCount := 0
	jsonPath := `{.status.conditions[?(@.type=="Available")].status}{.status.conditions[?(@.type=="Progressing")].status}{.status.conditions[?(@.type=="Degraded")].status}`

	e2e.Logf("waiting for CO %v back to normal status......", coName)
	waitErr := wait.PollImmediate(5*time.Second, totalWaitTime*time.Second, func() (bool, error) {
		status := getByJsonPath(oc, "default", "co/"+coName, jsonPath)
		primary := false
		printCount++
		if strings.Compare(status, "TrueFalseFalse") == 0 {
			count++
			if count == healthyThreshold {
				e2e.Logf("got %v successive good status (%v), the CO is stable!", count, status)
				primary = true
			} else {
				e2e.Logf("got %v successive good status (%v), try again...", count, status)
			}
		} else {
			count = 0
			if printCount%10 == 1 {
				e2e.Logf("CO status is still abnormal (%v), wait and try again...", status)
			}
		}
		return primary, nil
	})
	// for debugging: print all messages in co status.conditions
	if waitErr != nil {
		output := getByJsonPath(oc, "default", "co/"+coName, "{.status.conditions}")
		e2e.Logf("The co %v is abnormal and here is status: %v", coName, output)
		if coName == "ingress" {
			output, _ = oc.AsAdmin().WithoutNamespace().Run("describe").Args("-n", "openshift-ingress", "service", "router-default").Output()
			e2e.Logf("The output of describe router-default service: %v", output)
		}
	}
	exutil.AssertWaitPollNoErr(waitErr, fmt.Sprintf("reached max time allowed but CO %v is still abnoraml.", coName))
}

// this function ensure all cluster's operators become normal
func ensureAllClusterOperatorsNormal(oc *exutil.CLI, waitTime time.Duration) {
	opList := getClusterOperators(oc)
	for _, operator := range opList {
		ensureClusterOperatorNormal(oc, operator, 1, waitTime)
	}
}

// this function pick up those cluster operators in bad status
func checkAllClusterOperatorsStatus(oc *exutil.CLI) []string {
	badOpList := []string{}
	opList := getClusterOperators(oc)
	jsonPath := `-o=jsonpath={.status.conditions[?(@.type=="Available")].status}{.status.conditions[?(@.type=="Progressing")].status}{.status.conditions[?(@.type=="Degraded")].status}`
	for _, operator := range opList {
		searchLine, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("clusteroperator", operator, jsonPath).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if !strings.Contains(searchLine, "TrueFalseFalse") {
			badOpList = append(badOpList, operator)
		}
	}
	return badOpList
}

// to speed up the dns/coredns testing, just force only one dns-default pod in the cluster during the test
// find random linux node and add label "ne-dns-testing=true" to it, then patch spec.nodePlacement.nodeSelector
// please use func deleteDnsOperatorToRestore() for clear up.
func forceOnlyOneDnsPodExist(oc *exutil.CLI) string {
	ns := "openshift-dns"
	dnsPodLabel := "dns.operator.openshift.io/daemonset-dns=default"
	dnsNodeSelector := `[{"op":"replace", "path":"/spec/nodePlacement/nodeSelector", "value":{"ne-dns-testing":"true"}}]`
	// ensure no node with the label "ne-dns-testing=true"
	oc.AsAdmin().WithoutNamespace().Run("label").Args("node", "-l", "ne-dns-testing=true", "ne-dns-testing-").Execute()
	podList := getAllDNSPodsNames(oc)
	if len(podList) == 1 {
		e2e.Logf("Found only one dns-default pod and it looks like SNO cluster. Continue the test...")
	} else {
		dnsPodName := getRandomElementFromList(podList)
		nodeName, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("pods", dnsPodName, "-o=jsonpath={.spec.nodeName}", "-n", ns).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		e2e.Logf("Find random dns pod '%s' and its node '%s' which will be used for the following testing", dnsPodName, nodeName)
		// add special label "ne-dns-testing=true" to the node and force only one dns pod running on it
		_, err = oc.AsAdmin().WithoutNamespace().Run("label").Args("node", nodeName, "ne-dns-testing=true").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		patchGlobalResourceAsAdmin(oc, "dnses.operator.openshift.io/default", dnsNodeSelector)
		err1 := waitForResourceToDisappear(oc, ns, "pod/"+dnsPodName)
		if err1 != nil {
			output, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("pods", "-n", ns, "-l", dnsPodLabel).Output()
			e2e.Logf("All current dns pods are:\n%v", output)
		}
		exutil.AssertWaitPollNoErr(err1, fmt.Sprintf("max time reached but pod %s is not terminated", dnsPodName))
		err2 := waitForPodWithLabelReady(oc, ns, dnsPodLabel)
		if err2 != nil {
			output, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("pods", "-n", ns, "-l", dnsPodLabel).Output()
			e2e.Logf("All current dns pods are:\n%v", output)
		}
		exutil.AssertWaitPollNoErr(err2, fmt.Sprintf("max time reached but no dns pod ready"))
	}
	return getDNSPodName(oc)
}

func deleteDnsOperatorToRestore(oc *exutil.CLI) {
	_, err := oc.AsAdmin().WithoutNamespace().Run("delete").Args("dnses.operator.openshift.io/default").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	ensureClusterOperatorNormal(oc, "dns", 2, 120)
	// remove special label "ne-dns-testing=true" from the node
	oc.AsAdmin().WithoutNamespace().Run("label").Args("node", "-l", "ne-dns-testing=true", "ne-dns-testing-").Execute()
}

// this function is to get all dns pods' names
func getAllDNSPodsNames(oc *exutil.CLI) []string {
	ns := "openshift-dns"
	label := "dns.operator.openshift.io/daemonset-dns=default"
	dnsPods := getByLabelAndJsonPath(oc, ns, "pod", label, "{.items[*].metadata.name}")
	return strings.Split(dnsPods, " ")
}

// this function returns an element randomly from the given list
func getRandomElementFromList(list []string) string {
	seed := rand.New(rand.NewSource(time.Now().UnixNano()))
	index := seed.Intn(len(list))
	return list[index]
}

// this function is to check whether the given resource pod's are deleted or not
func waitForRangeOfPodsToDisappear(oc *exutil.CLI, resource string, podList []string) {
	for _, podName := range podList {
		err := waitForResourceToDisappear(oc, resource, "pod/"+podName)
		exutil.AssertWaitPollNoErr(err, fmt.Sprintf("%s pod %s is NOT deleted", resource, podName))
	}
}

// this function is to wait for the expStr appearing in the corefile of the coredns under all dns pods
func keepSearchInAllDNSPods(oc *exutil.CLI, podList []string, expStr string) {
	cmd := fmt.Sprintf("grep \"%s\" /etc/coredns/Corefile", expStr)
	o.Expect(podList).NotTo(o.BeEmpty())
	for _, podName := range podList {
		count := 0
		waitErr := wait.Poll(15*time.Second, 360*time.Second, func() (bool, error) {
			output, _ := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-dns", podName, "-c", "dns", "--", "bash", "-c", cmd).Output()
			count++
			primary := false
			if strings.Contains(output, expStr) {
				e2e.Logf("find " + expStr + " in the Corefile of pod " + podName)
				primary = true
			} else {
				// reduce the logs
				if count%2 == 1 {
					e2e.Logf("can't find " + expStr + " in the Corefile of pod " + podName + ", wait and try again...")
				}
			}
			return primary, nil
		})
		exutil.AssertWaitPollNoErr(waitErr, "can't find "+expStr+" in the Corefile of pod "+podName)
	}
}

// this function is to get desired logs from all dns pods
func searchLogFromDNSPods(oc *exutil.CLI, podList []string, searchStr string) string {
	o.Expect(podList).NotTo(o.BeEmpty())
	for _, podName := range podList {
		output, _ := oc.AsAdmin().WithoutNamespace().Run("logs").Args(podName, "-c", "dns", "-n", "openshift-dns").Output()
		outputList := strings.Split(output, "\n")
		for _, line := range outputList {
			if strings.Contains(line, searchStr) {
				return line
			}
		}
	}
	return "none"
}

func waitRouterLogsAppear(oc *exutil.CLI, routerpod, searchStr string) string {
	result := ""
	containerName := getByJsonPath(oc, "openshift-ingress", "pod/"+routerpod, "{.spec.containers[*].name}")
	logCmd := []string{routerpod, "-n", "openshift-ingress"}
	if strings.Contains(containerName, "logs") {
		logCmd = []string{routerpod, "-c", strings.Split(containerName, " ")[1], "-n", "openshift-ingress"}
	}
	err := wait.Poll(10*time.Second, 300*time.Second, func() (bool, error) {
		output, err := oc.AsAdmin().WithoutNamespace().Run("logs").Args(logCmd...).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		primary := false
		outputList := strings.Split(output, "\n")
		for _, line := range outputList {
			if strings.Contains(line, searchStr) {
				primary = true
				result = line
				e2e.Logf("the searchline has result:%v", line)
				break
			}
		}
		return primary, nil
	})
	exutil.AssertWaitPollNoErr(err, fmt.Sprintf("expected string \"%s\" is not found in the router pod's logs", searchStr))
	return result
}

// this function to get one dns pod's Corefile info related to the modified time, it looks like {{"dns-default-0001", "2021-12-30 18.011111 Modified"}}
func getOneCorefileStat(oc *exutil.CLI, dnspodname string) [][]string {
	attrList := [][]string{}
	cmd := "stat /etc/coredns/..data/Corefile | grep Modify"
	output, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-dns", dnspodname, "-c", "dns", "--", "bash", "-c", cmd).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	return append(attrList, []string{dnspodname, output})
}

// replace the coredns image that specified by co/dns, currently only for replacement of coreDNS-pod.yaml
func replaceCoreDnsImage(oc *exutil.CLI, file string) {
	coreDnsImage, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("co/dns", "-o=jsonpath={.status.versions[?(.name == \"coredns\")].version}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	result, err := exec.Command("bash", "-c", fmt.Sprintf(`grep "image: " %s`, file)).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("the result of grep command is: %s", result)
	if strings.Contains(string(result), coreDnsImage) {
		e2e.Logf("the image has been updated, no action and continue")
	} else {
		// use "|" as delimiter here since the image looks like
		// "quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:xxxxx"
		sedCmd := fmt.Sprintf(`sed -i'' -e 's|replaced-at-runtime|%s|g' %s`, coreDnsImage, file)
		_, err := exec.Command("bash", "-c", sedCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
	}
}

// this fucntion will return the master pod who has the virtual ip
func getVipOwnerPod(oc *exutil.CLI, ns string, podname []string, vip string) string {
	cmd := fmt.Sprintf("ip address |grep %s", vip)
	var primaryNode string
	for i := 0; i < len(podname); i++ {
		output, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", ns, podname[i], "--", "bash", "-c", cmd).Output()
		if len(podname) == 1 && output == "command terminated with exit code 1" {
			e2e.Failf("The given pod is not master")
		}
		if output == "command terminated with exit code 1" {
			e2e.Logf("This Pod %v does not have the VIP", podname[i])
		} else if strings.Contains(output, vip) {
			e2e.Logf("The pod owning the VIP is %v", podname[i])
			primaryNode = podname[i]
			break
		} else {
			o.Expect(err).NotTo(o.HaveOccurred())
		}
	}
	return primaryNode
}

// this function will remove the given element from the slice
func slicingElement(element string, podList []string) []string {
	var newPodList []string
	for index, pod := range podList {
		if pod == element {
			newPodList = append(podList[:index], podList[index+1:]...)
			break
		}
	}
	e2e.Logf("The remaining pod/s in the list is %v", newPodList)
	return newPodList
}

// this function checks whether given pod becomes primary or not
func waitForPrimaryPod(oc *exutil.CLI, ns string, pod string, vip string) {
	cmd := fmt.Sprintf("ip address |grep %s", vip)
	waitErr := wait.Poll(5*time.Second, 50*time.Second, func() (bool, error) {
		output, _ := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", ns, pod, "--", "bash", "-c", cmd).Output()
		primary := false
		if strings.Contains(output, vip) {
			e2e.Logf("The new pod %v is the master", pod)
			primary = true
		} else {
			e2e.Logf("pod failed to become master yet, retrying...the error is %v", output)
		}
		return primary, nil
	})
	// for debugging
	output1, _ := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", ns, pod, "--", "bash", "-c", "ip address").Output()
	exutil.AssertWaitPollNoErr(waitErr, fmt.Sprintf("max time reached, pod failed to become master and the entire ip details of the pod is %v", output1))
}

// this function will search the specific data from the given pod
func readPodData(oc *exutil.CLI, podname string, ns string, executeCmd string, searchString string) string {
	cmd := fmt.Sprintf("%s | grep \"%s\"", executeCmd, searchString)
	output, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", ns, podname, "--", "bash", "-c", cmd).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("the matching part is: %s", output)
	return output
}

// this function is a wrapper for polling `readPodData` function
func pollReadPodData(oc *exutil.CLI, ns, routername, executeCmd, searchString string) string {
	cmd := fmt.Sprintf("%s | grep \"%s\"", executeCmd, searchString)
	var output string
	var err error
	waitErr := wait.Poll(5*time.Second, 60*time.Second, func() (bool, error) {
		output, err = oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", ns, routername, "--", "bash", "-c", cmd).Output()
		if err != nil {
			e2e.Logf("failed to get search string: %v, retrying...", err)
			return false, nil
		}
		return true, nil
	})
	e2e.Logf("the matching part is: %s", output)
	exutil.AssertWaitPollNoErr(waitErr, fmt.Sprintf("reached max time allowed but cannot find the search string."))
	return output
}

// this function create external dns operator
func createExternalDNSOperator(oc *exutil.CLI) {
	buildPruningBaseDir := testdata.FixturePath("router", "extdns")
	operatorGroup := filepath.Join(buildPruningBaseDir, "operatorgroup.yaml")
	subscription := filepath.Join(buildPruningBaseDir, "subscription.yaml")
	nsOperator := filepath.Join(buildPruningBaseDir, "ns-external-dns-operator.yaml")
	operatorNamespace := "external-dns-operator"

	msg, err := oc.AsAdmin().WithoutNamespace().Run("apply").Args("-f", nsOperator).Output()
	e2e.Logf("err %v, msg %v", err, msg)
	msg, err = oc.AsAdmin().WithoutNamespace().Run("apply").Args("-f", operatorGroup).Output()
	e2e.Logf("err %v, msg %v", err, msg)

	// Deciding subscription need to be taken from which catalog
	output, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("-n", "openshift-marketplace", "catalogsource", "qe-app-registry").Output()
	if strings.Contains(output, "NotFound") {
		e2e.Logf("Warning: catalogsource/qe-app-registry is not installed, using redhat-operators instead")
		sedCmd := fmt.Sprintf(`sed -i'' -e 's/qe-app-registry/redhat-operators/g' %s`, subscription)
		_, err := exec.Command("bash", "-c", sedCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
	}

	msg, err = oc.AsAdmin().WithoutNamespace().Run("apply").Args("-f", subscription).Output()
	e2e.Logf("err %v, msg %v", err, msg)

	// checking subscription status
	errCheck := wait.Poll(10*time.Second, 180*time.Second, func() (bool, error) {
		subState, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("sub", "external-dns-operator", "-n", operatorNamespace, "-o=jsonpath={.status.state}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if strings.Compare(subState, "AtLatestKnown") == 0 {
			return true, nil
		}
		return false, nil
	})
	exutil.AssertWaitPollNoErr(errCheck, fmt.Sprintf("subscription external-dns-operator is not correct status"))

	// checking csv status
	csvName, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("sub", "external-dns-operator", "-n", operatorNamespace, "-o=jsonpath={.status.installedCSV}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	o.Expect(csvName).NotTo(o.BeEmpty())
	errCheck = wait.Poll(10*time.Second, 180*time.Second, func() (bool, error) {
		csvState, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("csv", csvName, "-n", operatorNamespace, "-o=jsonpath={.status.phase}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if strings.Compare(csvState, "Succeeded") == 0 {
			e2e.Logf("CSV check complete!!!")
			return true, nil
		}
		return false, nil

	})
	exutil.AssertWaitPollNoErr(errCheck, fmt.Sprintf("csv %v is not correct status", csvName))
}

// Skip the test if there is no 'qe-app-registry' or 'redhat-operators' catalogsource in the cluster
func skipMissingCatalogsource(oc *exutil.CLI) {
	catalogOutput, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("-n", "openshift-marketplace", "catalogsource").Output()
	if !strings.Contains(catalogOutput, "qe-app-registry") && !strings.Contains(catalogOutput, "redhat-operators") {
		g.Skip("Skip the test since there is no 'qe-app-registry' nor 'redhat-operators' catalogsource in the cluster")
	}
}

func deleteNamespace(oc *exutil.CLI, ns string) {
	err := oc.AdminKubeClient().CoreV1().Namespaces().Delete(context.Background(), ns, metav1.DeleteOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			err = nil
		}
	}
	o.Expect(err).NotTo(o.HaveOccurred())
	err = wait.PollUntilContextTimeout(context.Background(), 5*time.Second, 180*time.Second, true, func(context.Context) (done bool, err error) {
		_, err = oc.AdminKubeClient().CoreV1().Namespaces().Get(context.Background(), ns, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	})
	exutil.AssertWaitPollNoErr(err, fmt.Sprintf("Namespace %s is not deleted in 3 minutes", ns))
}

// Get OIDC from STS cluster
func getOidc(oc *exutil.CLI) string {
	oidc, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("authentication.config", "cluster", "-o=jsonpath={.spec.serviceAccountIssuer}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	oidc = strings.TrimPrefix(oidc, "https://")
	e2e.Logf("The OIDC of STS cluster is: %v", oidc)
	return oidc
}

// this function create aws-load-balancer-operator
func createAWSLoadBalancerOperator(oc *exutil.CLI) {
	buildPruningBaseDir := testdata.FixturePath("router", "awslb")
	operatorGroup := filepath.Join(buildPruningBaseDir, "operatorgroup.yaml")
	subscription := filepath.Join(buildPruningBaseDir, "subscription-src-qe.yaml")
	subSTS := filepath.Join(buildPruningBaseDir, "subscription-src-qe-sts.yaml")
	namespaceFile := filepath.Join(buildPruningBaseDir, "namespace.yaml")
	ns := "aws-load-balancer-operator"
	deployName := "deployment/aws-load-balancer-operator-controller-manager"

	msg, err := oc.AsAdmin().WithoutNamespace().Run("apply").Args("-f", namespaceFile).Output()
	e2e.Logf("err %v, msg %v", err, msg)

	if exutil.IsSTSCluster(oc) {
		e2e.Logf("This is STS cluster, create ALB operator and controller secrets via AWS SDK")
		prepareAllForStsCluster(oc)
	}

	msg, err = oc.AsAdmin().WithoutNamespace().Run("apply").Args("-f", operatorGroup).Output()
	e2e.Logf("err %v, msg %v", err, msg)

	// if qe-app-registry is not installed then replace the source to redhat-operators
	output, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("-n", "openshift-marketplace", "catalogsource", "qe-app-registry").Output()
	if strings.Contains(output, "NotFound") {
		e2e.Logf("Warning: catalogsource/qe-app-registry is not installed, using redhat-operators instead")
		sedCmd := fmt.Sprintf(`sed -i'' -e 's/qe-app-registry/redhat-operators/g' %s`, subscription)
		_, err := exec.Command("bash", "-c", sedCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		sedCmd = fmt.Sprintf(`sed -i'' -e 's/qe-app-registry/redhat-operators/g' %s`, subSTS)
		_, err = exec.Command("bash", "-c", sedCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
	}
	if exutil.IsSTSCluster(oc) {
		e2e.Logf("Updating and applying subcripton with Role ARN on STS cluster")
		sedCmd := fmt.Sprintf(`sed -i'' -e 's|fakeARN-for-albo|%s|g' %s`, os.Getenv("ALBO_ROLE_ARN"), subSTS)
		_, err := exec.Command("bash", "-c", sedCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		msg, err = oc.AsAdmin().WithoutNamespace().Run("apply").Args("-f", subSTS).Output()
		e2e.Logf("err %v, msg %v", err, msg)
	} else {
		msg, err = oc.AsAdmin().WithoutNamespace().Run("apply").Args("-f", subscription).Output()
		e2e.Logf("err %v, msg %v", err, msg)
	}

	// checking subscription status
	errCheck := wait.Poll(10*time.Second, 180*time.Second, func() (bool, error) {
		subState, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("sub", "aws-load-balancer-operator", "-n", ns, "-o=jsonpath={.status.state}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if strings.Compare(subState, "AtLatestKnown") == 0 {
			return true, nil
		}
		return false, nil
	})
	exutil.AssertWaitPollNoErr(errCheck, fmt.Sprintf("subscription aws-load-balancer-operator is not correct status"))

	// checking csv status
	csvName, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("sub", "aws-load-balancer-operator", "-n", ns, "-o=jsonpath={.status.installedCSV}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	o.Expect(csvName).NotTo(o.BeEmpty())
	errCheck = wait.Poll(10*time.Second, 180*time.Second, func() (bool, error) {
		csvState, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("csv", csvName, "-n", ns, "-o=jsonpath={.status.phase}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if strings.Compare(csvState, "Succeeded") == 0 {
			e2e.Logf("CSV check complete!!!")
			return true, nil
		}
		return false, nil
	})
	// output log of deployment/aws-load-balancer-operator-controller-manager for debugging
	if errCheck != nil {
		output, _ := oc.AsAdmin().WithoutNamespace().Run("logs").Args(deployName, "-n", ns, "--tail=10").Output()
		e2e.Logf("The logs of albo deployment: %v", output)
	}
	exutil.AssertWaitPollNoErr(errCheck, fmt.Sprintf("csv %v is not correct status", csvName))
}

func patchAlbControllerWithRoleArn(oc *exutil.CLI, ns string) {
	e2e.Logf("patching the ALB Controller with Role ARN on STS cluster")
	jsonPatch := fmt.Sprintf(`[{"op":"add","path":"/spec/credentialsRequestConfig","value":{"stsIAMRoleARN":%s}}]`, os.Getenv("ALBC_ROLE_ARN"))
	_, err := oc.AsAdmin().WithoutNamespace().Run("patch").Args("-n", ns, "awsloadbalancercontroller/cluster", "-p", jsonPatch, "--type=json").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
}

// get AWS outposts subnet so we can add annonation to ingress
func getOutpostSubnetId(oc *exutil.CLI) string {
	machineSet := clusterinfra.GetOneOutpostMachineSet(oc)
	subnetId, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("machineset", machineSet, "-n", "openshift-machine-api", "-o=jsonpath={.spec.template.spec.providerSpec.value.subnet.id}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("the outpost subnet is %v", subnetId)
	return subnetId
}

// this function check if the load balancer provisioned
func waitForLoadBalancerProvision(oc *exutil.CLI, ns string, ingressName string) {
	waitErr := wait.Poll(5*time.Second, 180*time.Second, func() (bool, error) {
		output, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("-n", ns, "ingress", ingressName, "-o=jsonpath={.status.loadBalancer.ingress}").Output()
		if output != "" && strings.Contains(output, "k8s-") {
			e2e.Logf("The load balancer is provisoned: %v", output)
			return true, nil
		}
		return false, nil
	})
	exutil.AssertWaitPollNoErr(waitErr, fmt.Sprintf("max time reached but the Load Balancer is not provisioned"))
}

// openssl generate the ca.key and ca.crt
func opensslNewCa(caKey, caCrt, caSubj string) {
	opensslCmd := fmt.Sprintf(`openssl req -x509 -newkey rsa:4096 -sha256 -days 365 -keyout %v -out %v -nodes -subj "%v"`, caKey, caCrt, caSubj)
	e2e.Logf("the openssl command is: %v", opensslCmd)
	_, err := exec.Command("bash", "-c", opensslCmd).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
}

// openssl generate the server CSR
func opensslNewCsr(serverKey, serverCsr, serverSubj string) {
	opensslCmd := fmt.Sprintf(`openssl req -newkey rsa:4096 -nodes -sha256 -keyout %v -out %v -subj "%v"`, serverKey, serverCsr, serverSubj)
	e2e.Logf("the openssl command is: %v", opensslCmd)
	_, err := exec.Command("bash", "-c", opensslCmd).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
}

// openssl sign the server CSR and generate server.crt
func opensslSignCsr(extfile, serverCsr, caCrt, caKey, serverCrt string) {
	opensslCmd := fmt.Sprintf(`openssl x509 -req -extfile <(printf "%v") -days 30 -in %v -CA %v -CAcreateserial -CAkey %v -out %v`, extfile, serverCsr, caCrt, caKey, serverCrt)
	e2e.Logf("the openssl command is: %v", opensslCmd)
	_, err := exec.Command("bash", "-c", opensslCmd).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
}

// wait until curling route returns expected output (check error as well)
// curl is executed on client outside the cluster
func waitForOutsideCurlContains(url string, curlOptions string, expected string) string {
	var output []byte
	cmd := fmt.Sprintf(`curl --connect-timeout 10 -s %s %s 2>&1`, curlOptions, url)
	e2e.Logf("the command is: %s", cmd)
	waitErr := wait.Poll(5*time.Second, 30*time.Second, func() (bool, error) {
		result, err := exec.Command("bash", "-c", cmd).Output()
		e2e.Logf("the result is: %s", result)
		output = result
		if err != nil {
			e2e.Logf("the error is: %v", err.Error())
			if strings.Contains(err.Error(), expected) {
				e2e.Logf("the expected string is included in err: %v", err)
				return true, nil
			} else {
				// route timeout case, curl returns an execution error which is expected
				if strings.Contains(err.Error(), expected) {
					e2e.Logf("Execution Error expected: %v", err)
					return true, nil
				}
				e2e.Logf("hit execution error: %v, retrying...", err)
				return false, nil
			}
		}
		if !strings.Contains(string(result), expected) {
			e2e.Logf("no expected string in the curl response: %s, retrying...", result)
			return false, nil
		}
		return true, nil
	})
	// for debugging: print verbose result of curl if timeout
	if waitErr != nil {
		debug_cmd := fmt.Sprintf(`curl --connect-time 10 -s -v %s %s 2>&1`, curlOptions, url)
		e2e.Logf("the debug command is: %s", debug_cmd)
		result, err := exec.Command("bash", "-c", debug_cmd).Output()
		e2e.Logf("debug: the result of curl is %s and err is %v", result, err)
	}
	exutil.AssertWaitPollNoErr(waitErr, fmt.Sprintf("max time reached but not get expected string"))
	return string(output)
}

// curl command with poll
func waitForCurl(oc *exutil.CLI, podName, baseDomain string, routestring string, searchWord string, controllerIP string) {
	e2e.Logf("Polling for curl command")
	var output string
	var err error
	waitErr := wait.Poll(5*time.Second, 30*time.Second, func() (bool, error) {
		if controllerIP != "" {
			route := routestring + baseDomain + ":80"
			toDst := routestring + baseDomain + ":80:" + controllerIP
			output, err = oc.Run("exec").Args(podName, "--", "curl", "-v", "http://"+route, "--resolve", toDst, "--connect-timeout", "10").Output()
		} else {
			curlCmd2 := routestring + baseDomain
			output, err = oc.Run("exec").Args(podName, "--", "curl", "-v", "http://"+curlCmd2, "--connect-timeout", "10").Output()
		}
		if err != nil {
			e2e.Logf("curl is not yet resolving, retrying...")
			return false, nil
		}
		if !strings.Contains(output, searchWord) {
			e2e.Logf("retrying...cannot find the searchWord '%s' in the output:- %v ", searchWord, output)
			return false, nil
		}
		return true, nil
	})
	exutil.AssertWaitPollNoErr(waitErr, fmt.Sprintf("max time reached but the route is not reachable"))
}

// used to send the nslookup command until the desired dns logs appear
func nslookupsAndWaitForDNSlog(oc *exutil.CLI, podName, searchLog string, dnsPodList []string, nslookupCmdPara ...string) string {
	e2e.Logf("Polling for executing nslookupCmd and waiting the dns logs appear")
	output := ""
	cmd := append([]string{podName, "--", "nslookup"}, nslookupCmdPara...)
	waitErr := wait.Poll(5*time.Second, 300*time.Second, func() (bool, error) {
		oc.Run("exec").Args(cmd...).Execute()
		output = searchLogFromDNSPods(oc, dnsPodList, searchLog)
		primary := false
		if len(output) > 1 && output != "none" {
			primary = true
		}
		return primary, nil
	})
	exutil.AssertWaitPollNoErr(waitErr, fmt.Sprintf("max time reached,but expected string \"%s\" is not found in the dns logs", searchLog))
	return output
}

// this function will get the route hostname
func getRouteHost(oc *exutil.CLI, ns, routeName string) string {
	host, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("route", routeName, "-n", ns, `-ojsonpath={.spec.host}`).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("the host of the route %v is %v.", routeName, host)
	return host
}

// this function will get the route detail
func getRoutes(oc *exutil.CLI, ns string) string {
	output, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("route", "-n", ns).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("oc get route: %v", output)
	return output
}

// this function will get the ingress detail
func getIngress(oc *exutil.CLI, ns string) string {
	output, err := oc.Run("get").Args("ingress", "-n", ns).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("oc get ingress: %v", output)
	return output
}

// this function will help to create Opaque secret using cert file and its key_name
func createGenericSecret(oc *exutil.CLI, ns, name, keyName, certFile string) {
	cmd := fmt.Sprintf(`--from-file=%s=%v`, keyName, certFile)
	_, err := oc.AsAdmin().WithoutNamespace().Run("create").Args(
		"secret", "generic", name, cmd, "-n", ns).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
}

// this function is to obtain the resource name like ingress's,route's name
func getResourceName(oc *exutil.CLI, namespace, resourceName string) []string {
	var resourceList []string
	resourceNames, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("-n", namespace, resourceName,
		"-ojsonpath={.items..metadata.name}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	resourceList = strings.Split(resourceNames, " ")
	e2e.Logf("The resource '%s' names are  %v ", resourceName, resourceList)
	return resourceList
}

// this function is used to check whether proxy is configured or not
func checkProxy(oc *exutil.CLI) bool {
	httpProxy, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("proxy", "cluster", "-o=jsonpath={.status.httpProxy}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	httpsProxy, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("proxy", "cluster", "-o=jsonpath={.status.httpsProxy}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	if httpProxy != "" || httpsProxy != "" {
		return true
	}
	return false
}

// this function will advertise unicast peers for Nutanix
func unicastIPFailover(oc *exutil.CLI, ns, failoverName string) {
	platformtype := exutil.CheckPlatform(oc)

	if platformtype == "nutanix" || platformtype == "none" {
		getPodListByLabel(oc, oc.Namespace(), "ipfailover=hello-openshift")
		workerIPAddress, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("nodes", "--selector=node-role.kubernetes.io/worker=", "-ojsonpath={.items[*].status.addresses[0].address}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		modifiedIPList := strings.Split(workerIPAddress, " ")
		if len(modifiedIPList) < 2 {
			e2e.Failf("There is not enough IP addresses to add as unicast peer")
		}
		ipList := strings.Join(modifiedIPList, ",")
		cmd := fmt.Sprintf("OPENSHIFT_HA_UNICAST_PEERS=%v", ipList)
		setEnvVariable(oc, ns, "deploy/"+failoverName, "OPENSHIFT_HA_USE_UNICAST=true")
		setEnvVariable(oc, ns, "deploy/"+failoverName, cmd)
	}
}

// this function is to retrieve the status of the route after using RouteSelectors
func checkRouteDetailsRemoved(oc *exutil.CLI, namespace, routeName, ingresscontrollerName string) {
	e2e.Logf("polling for route details")
	jsonPath := fmt.Sprintf(`{.status.ingress[?(@.routerName=="%s")]}`, ingresscontrollerName)
	waitErr := wait.Poll(5*time.Second, 150*time.Second, func() (bool, error) {
		status, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("route", "-n", namespace, routeName,
			"-ojsonpath="+jsonPath).Output()
		if err != nil {
			e2e.Logf("there is some execution error and it is  %v, retrying...", err)
			return false, nil
		}
		if strings.Contains(status, "Admitted") {
			e2e.Logf("the matched string is still in the logs, retrying...")
			return false, nil
		}
		e2e.Logf("The route status is cleared!")
		return true, nil
	})
	o.Expect(waitErr).NotTo(o.HaveOccurred(), "The route %s yielded unexpected results", routeName)
}

// used to execute a command on the internal or external client for the desired times
// the return was the output of the last successfully executed command, and a list of counters for the expected output:
// for example, if one expected item is matched for one time, the mathcing counter will be increased by 1, which is useful to test http cookie cases
// support checking an expected error when executed a command and the error occur
func repeatCmdOnClient(oc *exutil.CLI, cmd, expectOutput interface{}, duration time.Duration, repeatTimes int) (string, []int) {
	var (
		clientType       = "Internal"
		matchedTimesList = []int{}
		successCurlCount = 0
		matchedCount     = 0
		expectOutputList = []string{}
		output           = ""
	)

	cmdStr, ok := cmd.(string)
	if ok {
		clientType = "External"
	}
	cmdList, _ := cmd.([]string)

	expStr, ok := expectOutput.(string)
	if ok {
		expectOutputList = append(expectOutputList, expStr)
	}
	expList, ok := expectOutput.([]string)
	if ok {
		expectOutputList = expList
	}

	for i := 0; i < len(expectOutputList); i++ {
		matchedTimesList = append(matchedTimesList, 0)
	}

	e2e.Logf("Using client type: %v", clientType)
	e2e.Logf("The cmdStr (used by External client) is '%v' and cmdList (used by Internal client) is %v", cmdStr, cmdList)
	e2e.Logf("The expectOutputList is %v and initial matchedTimesList is %v", expectOutputList, matchedTimesList)

	waitErr := wait.Poll(1*time.Second, duration*time.Second, func() (bool, error) {
		isMatch := false
		if clientType == "Internal" {
			info, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args(cmdList...).Output()
			if err != nil {
				e2e.Logf("The error is: %v", err.Error())
				searchInfo := regexp.MustCompile(expectOutputList[0]).FindStringSubmatch(err.Error())
				if len(searchInfo) > 0 {
					e2e.Logf("The expected string is included in err: %v", err)
					return true, nil
				} else {
					e2e.Logf("Failed to execute cmd and got err %v, retrying...", err.Error())
					return false, nil
				}
			}
			output = info
		} else {
			info, err := exec.Command("bash", "-c", cmdStr).CombinedOutput()
			if err != nil {
				e2e.Logf("The error is: %v", err.Error())
				searchInfo := regexp.MustCompile(expectOutputList[0]).FindStringSubmatch(err.Error())
				if len(searchInfo) > 0 {
					e2e.Logf("The expected string is included in err: %v", err)
					return true, nil
				} else {
					e2e.Logf("Failed to execute cmd and got err %v, retrying...", err.Error())
					return false, nil
				}
			}
			output = string(info)
		}

		successCurlCount++
		e2e.Logf("Executed cmd for %v times on the client and got output: %s", successCurlCount, output)

		for i := 0; i < len(expectOutputList); i++ {
			searchInfo := regexp.MustCompile(expectOutputList[i]).FindStringSubmatch(output)
			if len(searchInfo) > 0 {
				isMatch = true
				matchedCount++
				matchedTimesList[i] = matchedTimesList[i] + 1
				break
			}
		}

		if isMatch {
			e2e.Logf("Successfully executed cmd for %v times on the client, expecting %v times", matchedCount, repeatTimes)
			if matchedCount == repeatTimes {
				return true, nil
			} else {
				return false, nil
			}
		} else {
			// if after executed the cmd, but could not get a output in the expectOutput list, decrease the successfully executed times
			successCurlCount--
			e2e.Logf("Failed to find a match in the output, retrying...")
			return false, nil
		}
	})

	e2e.Logf("The matchedTimesList is: %v", matchedTimesList)
	exutil.AssertWaitPollNoErr(waitErr, fmt.Sprintf("max time reached but can't execute the cmd successfully for the desired times"))

	// return the last succecessful curl output and the succecessful curl times list for the expected list
	return output, matchedTimesList
}

// used to execute a command on the internal or external client repeatly until the expected error occurs
// the return was the whole output of executing the command with the error occuring
func waitForErrorOccur(oc *exutil.CLI, cmd interface{}, expectedErrorInfo string, duration time.Duration) string {
	var (
		clientType = "Internal"
		output     = ""
	)

	cmdStr, ok := cmd.(string)
	if ok {
		clientType = "External"
	}
	cmdList, _ := cmd.([]string)

	e2e.Logf("the command is: %v", cmd)
	waitErr := wait.Poll(3*time.Second, duration*time.Second, func() (bool, error) {
		if clientType == "Internal" {
			info, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args(cmdList...).Output()
			output = info
			if err == nil {
				e2e.Logf("expected error %v not happened, retrying...", expectedErrorInfo)
				return false, nil
			}
		} else {
			info, err := exec.Command("bash", "-c", cmdStr).Output()
			output = string(info)
			if err == nil {
				e2e.Logf("expected error %v not happened, retrying...", expectedErrorInfo)
				return false, nil
			}
		}

		searchInfo := regexp.MustCompile(expectedErrorInfo).FindStringSubmatch(output)
		if len(searchInfo) > 0 {
			return true, nil
		} else {
			e2e.Logf("expected error %v not happened, retrying...", expectedErrorInfo)
			return false, nil
		}
	})

	exutil.AssertWaitPollNoErr(waitErr, fmt.Sprintf("max time reached but can't execute the cmd successfully in the desired time duration"))
	return output
}

// this function is to check whether given string is present or not in a list
func checkGivenStringPresentOrNot(shouldContain bool, iterateObject []string, searchString string) {
	if shouldContain {
		o.Expect(iterateObject).To(o.ContainElement(o.ContainSubstring(searchString)))
	} else {
		o.Expect(iterateObject).NotTo(o.ContainElement(o.ContainSubstring(searchString)))
	}
}

// this function is pollinng to check output which should contain the expected string
func waitForOutputContains(oc *exutil.CLI, ns, resourceName, jsonPath, expected string, args ...interface{}) {
	waitDuration := 180 * time.Second
	for _, arg := range args {
		duration, ok := arg.(time.Duration)
		if ok {
			waitDuration = duration
		}
	}

	waitErr := wait.PollImmediate(5*time.Second, waitDuration, func() (bool, error) {
		output := getByJsonPath(oc, ns, resourceName, jsonPath)
		if strings.Contains(output, expected) {
			return true, nil
		}
		e2e.Logf("The output of jsonpath does NOT contain the expected string: %v, retrying...", expected)
		return false, nil
	})
	exutil.AssertWaitPollNoErr(waitErr, fmt.Sprintf("max time reached but cannot find the expected string"))
}

// this function is pollinng to check output which should equal the expected string
func waitForOutputEquals(oc *exutil.CLI, ns, resourceName, jsonPath, expected string, args ...interface{}) {
	waitDuration := 180 * time.Second
	for _, arg := range args {
		duration, ok := arg.(time.Duration)
		if ok {
			waitDuration = duration
		}
	}

	waitErr := wait.PollImmediate(5*time.Second, waitDuration, func() (bool, error) {
		output := getByJsonPath(oc, ns, resourceName, jsonPath)
		if output == expected {
			return true, nil
		}
		e2e.Logf("The output of jsonpath does NOT equal the expected string: %v, retrying...", expected)
		return false, nil
	})
	exutil.AssertWaitPollNoErr(waitErr, fmt.Sprintf("max time reached but cannot find the expected string"))
}

// this function keep checking util the searching for the regular expression matches
func waitForOutputMatchRegexp(oc *exutil.CLI, ns, resourceName, jsonPath, regExpress string, args ...interface{}) string {
	result := "NotMatch"
	waitDuration := 180 * time.Second
	for _, arg := range args {
		duration, ok := arg.(time.Duration)
		if ok {
			waitDuration = duration
		}
	}

	wait.Poll(5*time.Second, waitDuration, func() (bool, error) {
		sourceRange := getByJsonPath(oc, ns, resourceName, jsonPath)
		searchRe := regexp.MustCompile(regExpress)
		searchInfo := searchRe.FindStringSubmatch(sourceRange)
		if len(searchInfo) > 0 {
			result = searchInfo[0]
			return true, nil
		}
		return false, nil
	})
	return result
}

// this function is pollinng to check output which should NOT contain the expected string
func waitForOutputNotContains(oc *exutil.CLI, ns, resourceName, jsonPath, expected string, args ...interface{}) {
	waitDuration := 180 * time.Second
	for _, arg := range args {
		duration, ok := arg.(time.Duration)
		if ok {
			waitDuration = duration
		}
	}

	waitErr := wait.PollImmediate(5*time.Second, waitDuration, func() (bool, error) {
		output := getByJsonPath(oc, ns, resourceName, jsonPath)
		if !strings.Contains(output, expected) {
			return true, nil
		}
		e2e.Logf("The output of jsonpath contained the expected string: %v, retrying...", expected)
		return false, nil
	})
	exutil.AssertWaitPollNoErr(waitErr, fmt.Sprintf("max time reached but still contained the expected string"))
}

// this function check output of oc describe command is polled
func waitForDescriptionContains(oc *exutil.CLI, ns, resourceName, value string) {
	n := 0
	waitErr := wait.PollImmediate(5*time.Second, 180*time.Second, func() (bool, error) {
		output, err := oc.AsAdmin().WithoutNamespace().Run("describe").Args("-n", ns, resourceName).Output()
		if err != nil {
			return false, err
		}
		n++
		if n%10 == 1 {
			e2e.Logf("the description is: %v", output)
		}
		if strings.Contains(output, value) {
			return true, nil
		}
		return false, nil
	})
	exutil.AssertWaitPollNoErr(waitErr, fmt.Sprintf("max time reached but the desired searchString does not appear"))
}

// this function will search in the polled and described resource details
func searchInDescribeResource(oc *exutil.CLI, resource, resourceName, match string) string {
	var output string
	var err error
	waitErr := wait.Poll(10*time.Second, 180*time.Second, func() (bool, error) {
		output, err = oc.AsAdmin().WithoutNamespace().Run("describe").Args(resource, resourceName).Output()
		if err != nil || output == "" {
			e2e.Logf("failed to get describe output: %v, retrying...", err)
			return false, nil
		}
		if !strings.Contains(output, match) {
			e2e.Logf("cannot find the matched string in the output, retrying...")
			return false, nil
		}
		return true, nil
	})
	exutil.AssertWaitPollNoErr(waitErr, fmt.Sprintf("reached max time allowed but cannot find the search string."))
	return output
}

// this function is to add taint to resource
func addTaint(oc *exutil.CLI, resource, resourceName, taint string) {
	output, err := oc.AsAdmin().WithoutNamespace().Run("adm").Args("taint", resource, resourceName, taint).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	o.Expect(output).To(o.ContainSubstring(resource + "/" + resourceName + " tainted"))
}

// this function is to remove the configured taint
func deleteTaint(oc *exutil.CLI, resource, resourceName, taint string) {
	output, err := oc.AsAdmin().WithoutNamespace().Run("adm").Args("taint", resource, resourceName, taint).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	o.Expect(output).To(o.ContainSubstring(resource + "/" + resourceName + " untainted"))
}

func waitCoBecomes(oc *exutil.CLI, coName string, waitTime int, expectedStatus map[string]string) error {
	return wait.Poll(10*time.Second, time.Duration(waitTime)*time.Second, func() (bool, error) {
		gottenStatus := getCoStatus(oc, coName, expectedStatus)
		eq := reflect.DeepEqual(expectedStatus, gottenStatus)
		if eq {
			e2e.Logf("Given operator %s becomes %s", coName, gottenStatus)
			return true, nil
		}
		return false, nil
	})
}

func getCoStatus(oc *exutil.CLI, coName string, statusToCompare map[string]string) map[string]string {
	newStatusToCompare := make(map[string]string)
	for key := range statusToCompare {
		args := fmt.Sprintf(`-o=jsonpath={.status.conditions[?(.type == '%s')].status}`, key)
		status, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("co", args, coName).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		newStatusToCompare[key] = status
	}
	return newStatusToCompare
}

// this function will check the status of dns record in ingress operator
func checkDnsRecordStatusOfIngressOperator(oc *exutil.CLI, dnsRecordsName, statusToSearch, stringToCheck string) []string {
	jsonPath := fmt.Sprintf(`{.status.zones[*].conditions[*].%s}`, statusToSearch)
	status := getByJsonPath(oc, "openshift-ingress-operator", "dnsrecords/"+dnsRecordsName, jsonPath)
	statusList := strings.Split(status, " ")
	for _, line := range statusList {
		o.Expect(stringToCheck).To(o.ContainSubstring(line))
	}
	return statusList
}

// this function is to check whether the DNS Zone details are present in ingresss operator records
func checkDnsRecordsInIngressOperator(oc *exutil.CLI, recordName, privateZoneId, publicZoneId string) {
	// Collecting zone details from ingress operator
	Zones := getByJsonPath(oc, "openshift-ingress-operator", "dnsrecords/"+recordName, "{.status.zones[*].dnsZone}")
	// check the private and public zone detail are matching
	o.Expect(Zones).To(o.ContainSubstring(privateZoneId))
	if publicZoneId != "" {
		o.Expect(Zones).To(o.ContainSubstring(publicZoneId))
	}
}

// retrieve the IPV6 or IPV4 public client address of a cluster
func getClientIP(oc *exutil.CLI, clusterType string) string {
	if strings.Contains(clusterType, "ipv6single") || strings.Contains(clusterType, "dualstack") {
		res, _ := http.Get("https://api64.ipify.org")
		result, _ := ioutil.ReadAll(res.Body)
		return string(result)
	} else {
		res, _ := http.Get("https://api.ipify.org")
		result, _ := ioutil.ReadAll(res.Body)
		return string(result)
	}
}

// This function checks the cookie file generated through curl command and confirms that the file contains what is expected
func checkCookieFile(fileDir string, expectedString string) {
	output, err := ioutil.ReadFile(fileDir)
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("the cookie file content is: %s", output)
	o.Expect(strings.Contains(string(output), expectedString)).To(o.BeTrue())
}

func checkIPStackType(oc *exutil.CLI) string {
	svcNetwork, err := oc.WithoutNamespace().AsAdmin().Run("get").Args("network.operator", "cluster", "-o=jsonpath={.spec.serviceNetwork}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	if strings.Count(svcNetwork, ":") >= 2 && strings.Count(svcNetwork, ".") >= 2 {
		return "dualstack"
	} else if strings.Count(svcNetwork, ":") >= 2 {
		return "ipv6single"
	} else if strings.Count(svcNetwork, ".") >= 2 {
		return "ipv4single"
	}
	return ""
}

// based on the orignal yaml file, this function is used to add some extra parameters behind the specified parameter, the return is the new file name
func addExtraParametersToYamlFile(originalFile, flagPara, AddedContent string) string {
	filePath, _ := filepath.Split(originalFile)
	newFile := filePath + getRandomString()
	originalFileContent, err := os.ReadFile(originalFile)
	o.Expect(err).NotTo(o.HaveOccurred())
	newFileContent := ""
	for _, line := range strings.Split(string(originalFileContent), "\n") {
		newFileContent = newFileContent + line + "\n"
		if strings.Contains(line, flagPara) {
			newFileContent = newFileContent + AddedContent
		}
	}
	os.WriteFile(newFile, []byte(newFileContent), 0644)
	return newFile
}

// based on the orignal file, this function is used to add some content into the file behind the desired specified parameter, the return is the new file name
// if there are quite few file lines include the specified parameter, use the seq parameter to choose the desired one
func addContenToFileWithMatchedOrder(originalFile, flagPara, AddedContent string, seq int) string {
	filePath, _ := filepath.Split(originalFile)
	newFile := filePath + getRandomString()
	originalFileContent, err := os.ReadFile(originalFile)
	o.Expect(err).NotTo(o.HaveOccurred())
	newFileContent := ""
	matchedTime := 0
	for _, line := range strings.Split(string(originalFileContent), "\n") {
		newFileContent = newFileContent + line + "\n"
		if strings.Contains(line, flagPara) {
			matchedTime++
			if matchedTime == seq {
				newFileContent = newFileContent + AddedContent
			}
		}
	}
	os.WriteFile(newFile, []byte(newFileContent), 0644)
	return newFile
}

// this function is to check whether the ingress canary route could be accessible from outside
func isCanaryRouteAvailable(oc *exutil.CLI) bool {
	routehost := getByJsonPath(oc, "openshift-ingress-canary", "route/canary", "{.status.ingress[0].host}")
	curlCmd := fmt.Sprintf(`curl https://%s -skI --connect-timeout 10`, routehost)
	_, matchedTimes := repeatCmdOnClient(oc, curlCmd, "200", 60, 1)
	if matchedTimes[0] == 1 {
		return true
	} else {
		return false
	}
}

func updateFilebySedCmd(file, toBeReplaced, newContent string) {
	sedCmd := fmt.Sprintf(`sed -i'' -e 's|%s|%s|g' %s`, toBeReplaced, newContent, file)
	_, err := exec.Command("bash", "-c", sedCmd).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
}

// this function returns IPv6 and IPv4 on dual stack and main IP in case of single stack (v4 or v6)
func getPodIP(oc *exutil.CLI, namespace string, podName string) []string {
	ipStack := checkIPStackType(oc)
	var podIp []string
	if (ipStack == "ipv6single") || (ipStack == "ipv4single") {
		podIp1, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("pod", "-n", namespace, podName, "-o=jsonpath={.status.podIPs[0].ip}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		e2e.Logf("The pod  %s IP in namespace %s is %q", podName, namespace, podIp1)
		podIp = append(podIp, podIp1)
	} else if ipStack == "dualstack" {
		podIp1, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("pod", "-n", namespace, podName, "-o=jsonpath={.status.podIPs[0].ip}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		e2e.Logf("The pod's %s 1st IP in namespace %s is %q", podName, namespace, podIp1)
		podIp2, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("pod", "-n", namespace, podName, "-o=jsonpath={.status.podIPs[1].ip}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		e2e.Logf("The pod's %s 2nd IP in namespace %s is %q", podName, namespace, podIp2)
		podIp = append(podIp, podIp1, podIp2)
	}
	return podIp
}

// used to get a microshift node's valid host interfaces and host IPs for router-default load balancer service
func getValidInterfacesAndIPs(addressInfo string) (intList, IPList []string) {
	intList = []string{}
	IPList = []string{}
	for _, line := range strings.Split(addressInfo, "\n") {
		// loopback address and link-local address will not used for the load balancer's IPs(updated for OCPBUGS-32946)
		if !strings.Contains(line, "inet 127") && !strings.Contains(line, "inet 169") {
			intIPList := strings.Split(strings.TrimRight(line, " "), " ")
			intName := intIPList[len(intIPList)-1]
			hostIP := regexp.MustCompile("([0-9\\.]+)/[0-9]+").FindStringSubmatch(line)[1]
			intList = append(intList, intName)
			IPList = append(IPList, hostIP)
		}
	}
	e2e.Logf("The valid host interfaces are %v", intList)
	e2e.Logf("The valid host IPs are %v", IPList)
	return intList, IPList
}

// used to get a microshift node's valid host IPv6 addresses
func getValidIPv6Addresses(addressInfo string) (IPList []string) {
	IPList = []string{}
	ipv6Re := regexp.MustCompile("([0-9a-zA-Z]+:[0-9a-zA-Z:]+)")
	for _, line := range strings.Split(addressInfo, "\n") {
		if !strings.Contains(line, "deprecated") {
			ipv6Info := ipv6Re.FindStringSubmatch(line)
			if len(ipv6Info) > 0 {
				IPList = append(IPList, ipv6Info[1])
			}
		}
	}
	return IPList
}

// used to backup the config.yaml file under a microshift node before the testing
func backupConfigYaml(oc *exutil.CLI, ns, caseID, nodeName string) {
	backupConfig := fmt.Sprintf(`
if test -f /etc/microshift/config.yaml ; then
    cp /etc/microshift/config.yaml /etc/microshift/config.yaml.backup%s
else
    touch /etc/microshift/config.yaml.no%s
fi
`, caseID, caseID)
	_, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", ns, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", backupConfig).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
}

// used to restore the config.yaml file under a microshift node after the testing
func restoreConfigYaml(oc *exutil.CLI, ns, caseID, nodeName string) {
	recoverCmd := fmt.Sprintf(`
if test -f /etc/microshift/config.yaml.no%s; then
    rm -f /etc/microshift/config.yaml
    rm -f /etc/microshift/config.yaml.no%s
elif test -f /etc/microshift/config.yaml.backup%s ; then
    rm -f /etc/microshift/config.yaml
    cp /etc/microshift/config.yaml.backup%s /etc/microshift/config.yaml
    rm -f /etc/microshift/config.yaml.backup%s
fi
`, caseID, caseID, caseID, caseID, caseID)
	defer func() {
		_, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", ns, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", recoverCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		restartMicroshiftService(oc, ns, nodeName)
	}()
}

// used to append ingress configurtion to the original config.yaml file under a microshift node during the testing
func appendIngressToConfigYaml(oc *exutil.CLI, ns, caseID, nodeName, ingressConfig string) {
	customConfig := fmt.Sprintf(`
rm /etc/microshift/config.yaml -f
if test -f /etc/microshift/config.yaml.backup%s ; then
    cp /etc/microshift/config.yaml.backup%s /etc/microshift/config.yaml
fi
cat >> /etc/microshift/config.yaml << EOF
%s
EOF`, caseID, caseID, ingressConfig)
	_, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", ns, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", customConfig).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	output, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", ns, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", "microshift show-config").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	o.Expect(output).NotTo(o.ContainSubstring("error: invalid configuration"))
	e2e.Logf("microshift show-config is: \n%v", output)
	restartMicroshiftService(oc, ns, nodeName)
}

func appendInvalidIngressConfigToConfigYaml(oc *exutil.CLI, ns, caseID, nodeName, ingressConfig string) string {
	customConfig := fmt.Sprintf(`
rm /etc/microshift/config.yaml -f
if test -f /etc/microshift/config.yaml.backup%s ; then
    cp /etc/microshift/config.yaml.backup%s /etc/microshift/config.yaml
fi      
cat >> /etc/microshift/config.yaml << EOF
%s      
EOF`, caseID, caseID, ingressConfig)
	_, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", ns, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", customConfig).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	output, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", ns, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", "microshift show-config").Output()
	o.Expect(err).To(o.HaveOccurred())
	e2e.Logf("microshift show-config is: \n%v", output)
	return output
}

func getRouterDeploymentGeneration(oc *exutil.CLI, deploymentName string) int {
	actualGen, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("deployment/"+deploymentName, "-n", "openshift-ingress", "-o=jsonpath={.metadata.generation}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	actualGenerationInt, _ := strconv.Atoi(actualGen)
	return actualGenerationInt
}

// Convert the given IPv6 string to IPv6 PTR record
// ie, from "fd03::a" to "a.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.3.0.d.f.ip6.arpa"
func convertV6AddressToPTR(ipv6Address string) string {
	addr, _ := netip.ParseAddr(ipv6Address)
	// expand the ipv6 '::' string with zeros and remove the semicolon
	v6AddWithoutSemicolon := strings.Join(strings.Split(addr.StringExpanded(), ":"), "")
	reversedString := reverseString(v6AddWithoutSemicolon)
	// split the string with dots and add another string (.ip6.arpa)
	PtrString := strings.Join(strings.SplitAfter(reversedString, ""), ".") + ".ip6.arpa"
	e2e.Logf("The PTR record is %s", PtrString)
	return PtrString
}

// this function is polling to check the output of the cmd executed on a debug node, which should contain the expected string
func waitForDebugNodeOutputContains(oc *exutil.CLI, ns, node string, cmdList []string, expectedString string, args ...interface{}) string {
	var output string
	count := 0
	waitDuration := 180 * time.Second
	for _, arg := range args {
		duration, ok := arg.(time.Duration)
		if ok {
			waitDuration = duration
		}
	}

	e2e.Logf("The expected string is: \n%s", expectedString)
	waitErr := wait.Poll(10*time.Second, waitDuration*time.Second, func() (bool, error) {
		// hostOutput, err := exutil.DebugNodeRetryWithOptionsAndChroot(oc, node, []string{}, "cat", "/etc/hosts")
		output, err := exutil.DebugNodeRetryWithOptionsAndChroot(oc, node, []string{"--quiet=true", "--to-namespace=" + ns}, cmdList...)
		o.Expect(err).NotTo(o.HaveOccurred())
		if count%5 == 0 {
			e2e.Logf("The output of the cmd executed on the debug node is:\n%s", output)
		}
		count++

		// Comparing the output
		if !strings.Contains(output, expectedString) {
			e2e.Logf("Failed to find the expected string, retring...")
			return false, nil
		}

		e2e.Logf(`Find the expected string in the debug node's output: %s`, output)
		return true, nil
	})

	exutil.AssertWaitPollNoErr(waitErr, fmt.Sprintf("Max time reached, but the expected string was not found by checking the output of cmd executed on the debug node"))
	return output
}

// Get clusterIP of a service
func getSvcClusterIPByName(oc *exutil.CLI, ns, serviceName string) string {
	clusterIP, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("-n", ns, "svc", serviceName, "-o=jsonpath={.spec.clusterIP}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("The '%s' service's clusterIP of '%s' namespace is: %v", serviceName, ns, clusterIP)
	return clusterIP
}

// Function to reverse a string
func reverseString(str string) (result string) {
	// iterate over str and prepend to result
	for _, i := range str {
		result = string(i) + result
	}
	return
}

// used to sort string type of slice or string which can be transformed to the slice
func getSortedString(obj interface{}) string {
	objList := []string{}
	str, ok := obj.(string)
	if ok {
		objList = strings.Split(str, " ")
	}
	strList, ok := obj.([]string)
	if ok {
		objList = strList
	}
	sort.Strings(objList)
	return strings.Join(objList, " ")
}

// due to OCPBUGS-45192, used to configure the firewall on the microshfit node to permit fd01::/48 for the load balancer
// bug fixed PR: https://github.com/openshift/microshift/pull/4268(permit fd01::/48)
// the fixed not working for all deployment for some reasons, so just added the rules explicitly here
// since the fw rules won't take effect immediately after configured(and this function is for the microshift traffic testing only),  just extend the duration of the curl request(this is also a methond to check whether the fw rules take effect)
func configFwForLB(oc *exutil.CLI, ns, nodeName, ip string) {
	if strings.Contains(strings.ToLower(ip), "fd01:") {
		ip6tablesCmd := fmt.Sprintf(`
ip6tables -A FORWARD -s fd01::/48 -j ACCEPT
ip6tables -A FORWARD -d fd01::/48 -j ACCEPT
ip6tables -A OUTPUT -s fd01::/48 -j ACCEPT
ip6tables -A OUTPUT -d fd01::/48 -j ACCEPT
`)

		output, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", ns, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", "ip6tables -L").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if !strings.Contains(strings.ToLower(output), "fd01:") {
			_, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", ns, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", ip6tablesCmd).Output()
			o.Expect(err).NotTo(o.HaveOccurred())
		}
	}
}

func checkNodeStatus(oc *exutil.CLI, nodeName string, expectedStatus string) {
	var expectedStatus1 string
	var statusOutput string
	var err error
	if expectedStatus == "Ready" {
		expectedStatus1 = "True"
	} else if expectedStatus == "NotReady" {
		expectedStatus1 = "Unknown"
	} else {
		err1 := fmt.Errorf("TBD supported node status")
		o.Expect(err1).NotTo(o.HaveOccurred())
	}
	errWait := wait.Poll(15*time.Second, 15*time.Minute, func() (bool, error) {
		statusOutput, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("nodes", nodeName, "-ojsonpath={.status.conditions[-1].status}").Output()
		if err != nil {
			return false, nil
		}
		if statusOutput != expectedStatus1 {
			return false, nil
		}
		return true, nil
	})
	if errWait != nil {
		e2e.Logf("Expect Node %s in state %v, kubelet status is %s with error", nodeName, expectedStatus, statusOutput, err.Error())
	}
	exutil.AssertWaitPollNoErr(errWait, fmt.Sprintf("Node %s is not in expected status %s", nodeName, expectedStatus))
}

func restartMicroshiftService(oc *exutil.CLI, ns, nodeName string) {
	// As restart the microshift service, the debug node pod will quit with error
	// debug pod in the default namespace won't be deleted automatically, so debug the node in another namepace
	oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", ns, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", "sudo systemctl restart microshift").Output()
	exec.Command("bash", "-c", "sleep 60").Output()
	checkNodeStatus(oc, nodeName, "Ready")
}

// the function will provide enough time for the egressfirewall to get applied
func waitEgressFirewallApplied(oc *exutil.CLI, efName, ns string) string {
	var output string
	checkErr := wait.Poll(10*time.Second, 60*time.Second, func() (bool, error) {
		output, efErr := oc.AsAdmin().WithoutNamespace().Run("get").Args("egressfirewall", "-n", ns, efName).Output()
		if efErr != nil {
			e2e.Logf("Failed to get egressfirewall %v, error: %s. Trying again", efName, efErr)
			return false, nil
		}
		if !strings.Contains(output, "EgressFirewall Rules applied") {
			e2e.Logf("The egressfirewall was not applied, trying again. \n %s", output)
			return false, nil
		}
		return true, nil
	})
	exutil.AssertWaitPollNoErr(checkErr, fmt.Sprintf("reached max time allowed but cannot find the egressfirewall details."))
	return output
}

func checkDomainReachability(oc *exutil.CLI, podName, ns, domainName string, passOrFail bool) {
	curlCmd := fmt.Sprintf("curl -s -I %s --connect-timeout 5 ", domainName)
	if passOrFail {
		_, err := e2eoutput.RunHostCmdWithRetries(ns, podName, curlCmd, 10*time.Second, 20*time.Second)
		o.Expect(err).NotTo(o.HaveOccurred())
		ipStackType := checkIPStackType(oc)
		if ipStackType == "dualstack" {
			curlCmd = fmt.Sprintf("curl -s -6 -I %s --connect-timeout 5", domainName)
			_, err := e2eoutput.RunHostCmdWithRetries(ns, podName, curlCmd, 10*time.Second, 20*time.Second)
			o.Expect(err).NotTo(o.HaveOccurred())
		}

	} else {
		o.Eventually(func() error {
			_, err := e2eoutput.RunHostCmd(ns, podName, curlCmd)
			return err
		}, "20s", "10s").Should(o.HaveOccurred())
	}
}

// Check whether the given cluster is a byo vpc cluster
func isByoVpcCluster(oc *exutil.CLI) bool {
	jsonPath := `{.items[*].spec.template.spec.providerSpec.value.subnet.id}`
	subnet, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args(
		"-n", "openshift-machine-api", "machinesets.machine.openshift.io", "-o=jsonpath="+jsonPath).Output()
	if len(subnet) > 0 {
		e2e.Logf("This is a byo vpc cluster")
		return true
	}
	return false
}

// get list of all private subnets from machinesets, it might contains dup subnets or name without "private"
// this function will not work on Hypershift cluster
func getRawPrivateSubnetList(oc *exutil.CLI) []string {
	var jsonPath string
	if isByoVpcCluster(oc) {
		jsonPath = `{.items[*].spec.template.spec.providerSpec.value.subnet.id}`
	} else {
		jsonPath = `{.items[*].spec.template.spec.providerSpec.value.subnet.filters[].values[]}`
	}
	privateSubnets := getByJsonPath(oc, "openshift-machine-api", "machinesets.machine.openshift.io", jsonPath)
	privateSubnetList := strings.Split(privateSubnets, " ")
	e2e.Logf("The raw private subnet list from machinesets is: %v", privateSubnetList)
	return privateSubnetList
}

// convert private subnet list to public subnet list
// this func is used by AWS subnets/EIPs feature, ignore uncommon and duplicated subnets
func getPublicSubnetList(oc *exutil.CLI) []string {
	var publicSubnetList []string
	for _, subnet := range getRawPrivateSubnetList(oc) {
		if !strings.Contains(subnet, "subnet-private") {
			e2e.Logf("Warning: found subnet without private keyword: %v, ignore it", subnet)
			continue
		}
		// example subnet: ci-op-iip84q8t-3ca97-8fqzp-subnet-private-us-east-1a
		if matched, _ := regexp.MatchString(".*-private-([a-z]+)-([a-z]+)-([0-9a-z]+)$", subnet); !matched {
			e2e.Logf("Warning: found uncommon subnet: %v, ignore it", subnet)
			continue
		}
		publicSubnet := fmt.Sprintf(`"%s"`, strings.Replace(subnet, "subnet-private", "subnet-public", -1))
		if !slices.Contains(publicSubnetList, publicSubnet) {
			e2e.Logf("Got new valid public subnet: %v, append it", publicSubnet)
			publicSubnetList = append(publicSubnetList, publicSubnet)
		} else {
			e2e.Logf("Warning: found duplicated public subnet: %v, ignore it", publicSubnet)
		}
	}
	e2e.Logf("The public subnet list generated from private is: %v", publicSubnetList)
	return publicSubnetList
}

// for DCM testing, Check whether a new static endpoint is added to the backend
func isNewStaticEPAdded(initSrvStates, currentSrvStates string) bool {
	upEpReg := regexp.MustCompile("([0-9\\.a-zA-Z:]+ UP)")
	initUpEps := ""
	for _, entry := range strings.Split(initSrvStates, "\n") {
		if len(upEpReg.FindStringSubmatch(entry)) > 1 {
			initUpEps = initUpEps + upEpReg.FindStringSubmatch(entry)[1] + " "
		}
	}

	for _, entry := range strings.Split(currentSrvStates, "\n") {
		if len(upEpReg.FindStringSubmatch(entry)) > 1 && !strings.Contains(entry, "dynamic-pod") {
			if !strings.Contains(initUpEps, upEpReg.FindStringSubmatch(entry)[1]) {
				e2e.Logf("new static endpoint %s is added to the backend", upEpReg.FindStringSubmatch(entry)[1])
				return true
			}
		}
	}
	e2e.Logf("no new static endpoint is added to the backend")
	return false
}

// for DCM testing, scale Deployment
func scaleDeploy(oc *exutil.CLI, ns, deployName string, num int) []string {
	expReplicas := strconv.Itoa(num)
	if num == 0 {
		expReplicas = ""
	}
	_, err := oc.AsAdmin().WithoutNamespace().Run("scale").Args("-n", ns, "deployment/"+deployName, "--replicas="+strconv.Itoa(num)).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	waitForOutputEquals(oc, ns, "deployment/"+deployName, "{.status.availableReplicas}", expReplicas)
	podList, err := exutil.GetAllPodsWithLabel(oc, ns, "name="+deployName)
	o.Expect(err).NotTo(o.HaveOccurred())
	return podList
}

// for DCM testing, check route's backend configuration
func checkDcmBackendCfg(oc *exutil.CLI, routerpod, backend string) {
	dynamicPod := `server-template _dynamic-pod- 1-1.+check disabled`
	if strings.Contains(backend, "be_secure") {
		dynamicPod = `server _dynamic-pod-1.+disabled check.+verifyhost service.+`
	}

	backendCfg := getBlockConfig(oc, routerpod, backend)
	o.Expect(backendCfg).Should(o.And(
		o.MatchRegexp(`server pod:.+`),
		o.MatchRegexp(dynamicPod),
		o.MatchRegexp(`dynamic-cookie-key [0-9a-zA-A]+`)))

	// passthrough route hasn't the dynamic cookie
	if !strings.Contains(backend, "be_tcp") {
		o.Expect(backendCfg).To(o.MatchRegexp(`cookie.+dynamic`))
	}
}

// for DCM testing, check UP endpoint of the deployment
func checkDcmUpEndpoints(oc *exutil.CLI, routerpod, socatCmd string, replicasNum int) string {
	currentSrvStates, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", routerpod, "--", "bash", "-c", socatCmd).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	upEpNum := strings.Count(currentSrvStates, "UP")
	o.Expect(upEpNum).To(o.Equal(replicasNum))
	return currentSrvStates
}

// for DCM testing, check whether there are router reloaded logs as expected
func checkRouterReloadedLogs(oc *exutil.CLI, routerpod string, initReloadedNum int, initSrvStates, currentSrvStates string) int {
	isNewEPAdded := isNewStaticEPAdded(initSrvStates, currentSrvStates)
	log, err := oc.AsAdmin().WithoutNamespace().Run("logs").Args("-n", "openshift-ingress", routerpod).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	currentReloadedNum := strings.Count(log, `"msg"="router reloaded" "logger"="template" "output"=`)
	if isNewEPAdded {
		o.Expect(currentReloadedNum > initReloadedNum).To(o.BeTrue())
	} else {
		e2e.Logf("initReloadedNum is: %v", initReloadedNum)
		e2e.Logf("currentReloadedNum is: %v", currentReloadedNum)
		o.Expect(currentReloadedNum-initReloadedNum <= 1).To(o.BeTrue())
	}
	return currentReloadedNum
}

// for DCM testing, check whether all the deployment pods are accessible or not
func checkDcmServersAccessible(oc *exutil.CLI, curlCmd, podList []string, duration time.Duration, repeatTimes int) {
	_, result := repeatCmdOnClient(oc, curlCmd, podList, duration, repeatTimes)
	for i := 0; i < len(podList); i++ {
		o.Expect(result[i] > 0).To(o.BeTrue())
	}
}

// check if default ingresscontroller using internal LB, before calling this need to check if Cloud platforms
func isInternalLBScopeInDefaultIngresscontroller(oc *exutil.CLI) bool {
	lbScope := getByJsonPath(oc, "openshift-ingress-operator", "ingresscontroller/default", "{.status.endpointPublishingStrategy.loadBalancer.scope}")
	if strings.Compare(lbScope, "Internal") == 0 {
		e2e.Logf("The default ingresscontroller LB scope is Internal")
		return true
	}
	e2e.Logf("The default ingresscontroller LB scope is %v", lbScope)
	return false
}
