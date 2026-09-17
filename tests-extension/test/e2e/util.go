package router

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	exutil "github.com/openshift/origin/test/extended/util"
	compat_otp "github.com/openshift/origin/test/extended/util/compat_otp"
	"k8s.io/apimachinery/pkg/util/wait"
	e2e "k8s.io/kubernetes/test/e2e/framework"
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

type routeDescription struct {
	name      string
	namespace string
	domain    string
	subDomain string
	template  string
}

type webServerDeployDescription struct {
	deployName      string
	svcSecureName   string
	svcUnsecureName string
	template        string
	namespace       string
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

// Create route object from template.
func (route *routeDescription) create(oc *exutil.CLI) {
	err := createResourceToNsFromTemplate(oc, route.namespace, "--ignore-unknown-parameters=true", "-f", route.template, "-p", "SUBDOMAIN_NAME="+route.subDomain, "NAMESPACE="+route.namespace, "DOMAIN="+route.domain)
	o.Expect(err).NotTo(o.HaveOccurred())
}

// Create web server deployment from template
func (websrvdeploy *webServerDeployDescription) create(oc *exutil.CLI) {
	err := createResourceToNsFromTemplate(oc, websrvdeploy.namespace, "--ignore-unknown-parameters=true", "-f", websrvdeploy.template, "-p", "DEPLOY_NAME="+websrvdeploy.deployName, "SVC_SECURE_NAME="+websrvdeploy.svcSecureName, "SVC_UNSECURE_NAME="+websrvdeploy.svcUnsecureName)
	o.Expect(err).NotTo(o.HaveOccurred())
}

func (websrvdeploy *webServerDeployDescription) delete(oc *exutil.CLI) error {
	return oc.AsAdmin().WithoutNamespace().Run("delete").Args("-n", websrvdeploy.namespace, "deployment", websrvdeploy.deployName).Execute()
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

func getBaseDomain(oc *exutil.CLI) string {
	var basedomain string

	basedomain, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("dns.config/cluster", "-o=jsonpath={.spec.baseDomain}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("the base domain of the cluster: %v", basedomain)
	return basedomain
}

func getSAToken(oc *exutil.CLI, sa, ns string) (string, error) {
	e2e.Logf("Getting a token assgined to specific serviceaccount from %s namespace...", ns)
	token, err := oc.AsAdmin().WithoutNamespace().Run("create").Args("token", sa, "-n", ns).Output()
	if err != nil {
		if strings.Contains(token, "unknown command") {
			e2e.Logf("oc create token is not supported by current client, use oc sa get-token instead")
			token, err = oc.AsAdmin().WithoutNamespace().Run("sa").Args("get-token", sa, "-n", ns).Output()
		} else {
			return "", err
		}
	}

	return token, err
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
	compat_otp.AssertWaitPollNoErr(err, fmt.Sprintf("max time reached but ingresscontroller %v is not available", icName))
}

func ensureRouteIsAdmittedByIngressController(oc *exutil.CLI, ns, routeName, icName string) {
	jsonPath := fmt.Sprintf(`{.status.ingress[?(@.routerName=="%s")].conditions[?(@.type=="Admitted")].status}`, icName)
	waitForOutputEquals(oc, ns, "route/"+routeName, jsonPath, "True")
}

func getOnePodNameByLabel(oc *exutil.CLI, ns, label string) string {
	podName, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("pods", "-l", label, "-o=jsonpath={.items[0].metadata.name}", "-n", ns).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("the one pod with label %v is %v", label, podName)
	return podName
}

// GetOneNewRouterPodFromRollingUpdate immediately after/during deployment rolling update, don't care the previous pod status
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
	compat_otp.AssertWaitPollNoErr(waitErr, fmt.Sprintf("reached max time allowed but NewReplicaSet not found"))
	e2e.Logf("the new ReplicaSet labels is %s", rsLabel)
	err := waitForPodWithLabelReady(oc, ns, rsLabel)
	if err != nil {
		output, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("pods", "-n", ns).Output()
		e2e.Logf("All current router pods are:\n%v", output)
	}
	compat_otp.AssertWaitPollNoErr(err, "the new router pod failed to be ready within allowed time!")
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
	compat_otp.AssertWaitPollNoErr(waitErr, fmt.Sprintf("max time reached and the expected deployment generation is %v but got %v", expectGeneration, actualGeneration))
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
	compat_otp.AssertWaitPollNoErr(err, fmt.Sprintf("max time reached but the pods with label %v are not ready", label))
}

// Wait for the named resource is disappeared, e.g. used while router deployment rolled out
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

// To use createResourceFromFile function to create resources from files like web-server-rc.yaml and web-server-signed-deploy.yaml
func createResourceFromWebServer(oc *exutil.CLI, ns, file, srvrcInfo string) []string {
	createResourceFromFile(oc, ns, file)
	err := waitForPodWithLabelReady(oc, ns, "name="+srvrcInfo)
	compat_otp.AssertWaitPollNoErr(err, "backend server pod failed to be ready state within allowed time!")
	srvPodList := getPodListByLabel(oc, ns, "name="+srvrcInfo)
	return srvPodList
}

// For admin user to create/delete resources in the specified namespace from the file (not template)
// operator, should be create or delete
func operateResourceFromFile(oc *exutil.CLI, oper, ns, file string) {
	err := oc.AsAdmin().WithoutNamespace().Run(oper).Args("-f", file, "-n", ns).Execute()
	o.Expect(err).NotTo(o.HaveOccurred())
}

// For Admin to patch a resource in the specified namespace with 'merge' type
func patchResourceAsAdmin(oc *exutil.CLI, ns, resource, patch string) {
	patchResourceAsAdminAnyType(oc, ns, resource, patch, "merge")
}

// For Admin to patch a resource in the specified namespace with any type
// Type can be any like 'merge', 'json' etc
func patchResourceAsAdminAnyType(oc *exutil.CLI, ns, resource, patch, typ string) {
	err := oc.AsAdmin().WithoutNamespace().Run("patch").Args(resource, "-p", patch, "--type="+typ, "-n", ns).Execute()
	o.Expect(err).NotTo(o.HaveOccurred())
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

// This function will read the annotation from the given resource
func getAnnotation(oc *exutil.CLI, ns, resource, resourceName string) string {
	findAnnotation, err := oc.AsAdmin().WithoutNamespace().Run("get").Args(
		resource, resourceName, "-n", ns, "-o=jsonpath={.metadata.annotations}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	return findAnnotation
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

// This function get resource using label and filtered by jsonpath
func getByLabelAndJsonPath(oc *exutil.CLI, ns, resource, label, jsonPath string) string {
	output, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("-n", ns, resource, "-l", label, "-ojsonpath="+jsonPath).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("the output filtered by label and jsonpath is: %v", output)
	return output
}

// For collecting a single pod name for general use.
// Usage example: podname := getOneRouterPodNameByIC(oc, "default/labelname")
// Note: it might get wrong pod which will be terminated during deployment rolling update
func getOneRouterPodNameByIC(oc *exutil.CLI, icname string) string {
	podName, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("pods", "-l", "ingresscontroller.operator.openshift.io/deployment-ingresscontroller="+icname, "-o=jsonpath={.items[0].metadata.name}", "-n", "openshift-ingress").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("the result of router pod name: %v", podName)
	return podName
}

// To check the route data is present in the haproxy.config
// BlockCfgStart is the used to get the bulk config from the getBlockConfig function
// SearchList is used to locate the specified route config
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

	compat_otp.AssertWaitPollNoErr(waitErr, fmt.Sprintf("Reached max time allowed but the given string was not found in haproxy.config"))
	e2e.Logf("The part of haproxy.config that matching \"%s\" is:\n%v", blockCfgStart, haproxyCfg)
	return haproxyCfg
}

// To check the route data is not present in the haproxy.config
// BlockCfgStart is the used to get the bulk config from the getBlockConfig function
// SearchList is used to locate the specified route config
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

	compat_otp.AssertWaitPollNoErr(waitErr, fmt.Sprintf("Reached max time allowed but given string is still present in haproxy.config"))
	e2e.Logf("The part of haproxy.config that matching \"%s\" is:\n%v", blockCfgStart, haproxyCfg)
	return haproxyCfg
}

// Used to get block content of haproxy.conf, for example, get one route's whole backend's configuration specified by searchString(for exmpale: "be_edge_http:" + ns + ":r1-edg")
func getBlockConfig(oc *exutil.CLI, routerPodName, searchString string) string {
	// Use deployment reference for exec to avoid stale pod name issues during
	// rolling updates (same technique used in openshift-tests-private).
	execTarget := routerPodName
	parts := strings.Split(routerPodName, "-")
	if len(parts) >= 4 && parts[0] == "router" {
		icName := strings.Join(parts[1:len(parts)-2], "-")
		execTarget = "deploy/router-" + icName
	}
	output, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", execTarget, "--", "bash", "-c", "cat haproxy.config").Output()
	o.Expect(err).NotTo(o.HaveOccurred(), "get the content of haproxy.config failed")
	// Scope search to test namespace to prevent cross-test contamination
	// When tests run in parallel (e.g., "route-edge" matching "route-edge15873" from a different test's namespace).
	ns := oc.Namespace()
	result := ""
	flag := 0
	startIndex := 0
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, searchString) {
			if flag == 0 && ns != "" && !strings.Contains(searchString, ns) && !strings.Contains(line, ns) {
				continue
			}
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

// For collecting information from router pod [usage example: readRouterPodData(oc, podname, executeCmd, "search string")] .
// NOTE: This requires getOneRouterPodNameByIC function to collect the podname variable first!
func readRouterPodData(oc *exutil.CLI, routername, executeCmd string, searchString string) string {
	// Use deployment reference for exec to avoid stale pod name issues during
	// rolling updates (same technique used in openshift-tests-private).
	execTarget := routername
	parts := strings.Split(routername, "-")
	if len(parts) >= 4 && parts[0] == "router" {
		icName := strings.Join(parts[1:len(parts)-2], "-")
		execTarget = "deploy/router-" + icName
	}
	output := readPodData(oc, execTarget, "openshift-ingress", executeCmd, searchString)
	return output
}

// To Collect ingresscontroller domain name
func getIngressctlDomain(oc *exutil.CLI, icname string) string {
	var ingressctldomain string
	ingressctldomain, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("ingresscontroller", icname, "--namespace=openshift-ingress-operator", "-o=jsonpath={.status.domain}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("the domain for the ingresscontroller is : %v", ingressctldomain)
	return ingressctldomain
}

// This function helps to get the ipv4 address of the given pod
func getPodv4Address(oc *exutil.CLI, podName, namespace string) string {
	podIPv4, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("pod", podName, "-n", namespace, "-o=jsonpath={.status.podIP}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("IP of the %s pod in namespace %s is %q ", podName, namespace, podIPv4)
	return podIPv4
}

// This function is to obtain the pod name based on the particular label
func getPodListByLabel(oc *exutil.CLI, namespace string, label string) []string {
	var podList []string
	podNameAll, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("-n", namespace, "pod", "-l", label, "-ojsonpath={.items..metadata.name}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	podList = strings.Split(podNameAll, " ")
	e2e.Logf("The pod list is %v", podList)
	return podList
}

// Wait for the cluster operator back to normal status ("True False False")
// Wait until get the specified number of successive normal status, which is defined by healthyThreshold and totalWaitTime
// HealthyThreshold: max rounds for checking an CO, int type, and no less than 1
// TotalWaitTime: total checking time, time.Duration type, and no less than 1
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
	compat_otp.AssertWaitPollNoErr(waitErr, fmt.Sprintf("reached max time allowed but CO %v is still abnoraml.", coName))
}

// This function will search the specific data from the given pod
func readPodData(oc *exutil.CLI, podname string, ns string, executeCmd string, searchString string) string {
	cmd := fmt.Sprintf("%s | grep \"%s\"", executeCmd, searchString)
	output, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", ns, podname, "--", "bash", "-c", cmd).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("the matching part is: %s", output)
	return output
}

// Tait until curling route returns expected output (check error as well)
// Curl is executed on client outside the cluster
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
	// For debugging: print verbose result of curl if timeout
	if waitErr != nil {
		debug_cmd := fmt.Sprintf(`curl --connect-timeout 10 -s -v %s %s 2>&1`, curlOptions, url)
		e2e.Logf("the debug command is: %s", debug_cmd)
		result, err := exec.Command("bash", "-c", debug_cmd).Output()
		e2e.Logf("debug: the result of curl is %s and err is %v", result, err)
	}
	compat_otp.AssertWaitPollNoErr(waitErr, fmt.Sprintf("max time reached but not get expected string"))
	return string(output)
}

// Curl command with poll
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
	compat_otp.AssertWaitPollNoErr(waitErr, fmt.Sprintf("max time reached but the route is not reachable"))
}

// This function will get the route hostname
func getRouteHost(oc *exutil.CLI, ns, routeName string) string {
	host, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("route", routeName, "-n", ns, `-ojsonpath={.spec.host}`).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("the host of the route %v is %v.", routeName, host)
	return host
}

// This function will get the route detail
func getRoutes(oc *exutil.CLI, ns string) string {
	output, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("route", "-n", ns).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("oc get route: %v", output)
	return output
}

// This function is to obtain the resource name like ingress's,route's name
func getResourceName(oc *exutil.CLI, namespace, resourceName string) []string {
	var resourceList []string
	resourceNames, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("-n", namespace, resourceName,
		"-ojsonpath={.items..metadata.name}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	resourceList = strings.Split(resourceNames, " ")
	e2e.Logf("The resource '%s' names are  %v ", resourceName, resourceList)
	return resourceList
}

// This function is used to check whether proxy is configured or not
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

// This function is to retrieve the status of the route after using RouteSelectors
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

// Used to execute a command on the internal or external client for the desired times
// The return was the output of the last successfully executed command, and a list of counters for the expected output:
// For example, if one expected item is matched for one time, the mathcing counter will be increased by 1, which is useful to test http cookie cases
// Support checking an expected error when executed a command and the error occur
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
	compat_otp.AssertWaitPollNoErr(waitErr, fmt.Sprintf("max time reached but can't execute the cmd successfully for the desired times"))

	// return the last succecessful curl output and the succecessful curl times list for the expected list
	return output, matchedTimesList
}

// This function is to check whether given string is present or not in a list
func checkGivenStringPresentOrNot(shouldContain bool, iterateObject []string, searchString string) {
	if shouldContain {
		o.Expect(iterateObject).To(o.ContainElement(o.ContainSubstring(searchString)))
	} else {
		o.Expect(iterateObject).NotTo(o.ContainElement(o.ContainSubstring(searchString)))
	}
}

// This function is pollinng to check output which should contain the expected string
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
	compat_otp.AssertWaitPollNoErr(waitErr, fmt.Sprintf("max time reached but cannot find the expected string"))
}

// This function is polling to check output which should equal the expected string
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
	compat_otp.AssertWaitPollNoErr(waitErr, fmt.Sprintf("max time reached but cannot find the expected string"))
}

// This function keep checking util the searching for the regular expression matches
func waitForOutputMatchRegexp(oc *exutil.CLI, ns, resourceName, jsonPath, regExpress string, args ...interface{}) string {
	result := "NotMatch"
	waitDuration := 180 * time.Second
	for _, arg := range args {
		duration, ok := arg.(time.Duration)
		if ok {
			waitDuration = duration
		}
	}

	waitErr := wait.Poll(5*time.Second, waitDuration, func() (bool, error) {
		sourceRange := getByJsonPath(oc, ns, resourceName, jsonPath)
		searchRe := regexp.MustCompile(regExpress)
		searchInfo := searchRe.FindStringSubmatch(sourceRange)
		if len(searchInfo) > 0 {
			result = searchInfo[0]
			return true, nil
		}
		return false, nil
	})
	if waitErr != nil {
		e2e.Logf("waitForOutputMatchRegexp timed out waiting for %s %s to match %s", resourceName, jsonPath, regExpress)
	}
	return result
}

// This function will search in the polled and described resource details
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
	compat_otp.AssertWaitPollNoErr(waitErr, fmt.Sprintf("reached max time allowed but cannot find the search string."))
	return output
}

// This function checks the cookie file generated through curl command and confirms that the file contains what is expected
func checkCookieFile(fileDir string, expectedString string) {
	output, err := ioutil.ReadFile(fileDir)
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("the cookie file content is: %s", output)
	o.Expect(strings.Contains(string(output), expectedString)).To(o.BeTrue())
}

func updateFilebySedCmd(file, toBeReplaced, newContent string) {
	sedCmd := fmt.Sprintf(`sed -i'' -e 's|%s|%s|g' %s`, toBeReplaced, newContent, file)
	_, err := exec.Command("bash", "-c", sedCmd).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
}

// For DCM testing, scale Deployment
func scaleDeploy(oc *exutil.CLI, ns, deployName string, num int) []string {
	expReplicas := strconv.Itoa(num)
	if num == 0 {
		expReplicas = ""
	}
	_, err := oc.AsAdmin().WithoutNamespace().Run("scale").Args("-n", ns, "deployment/"+deployName, "--replicas="+strconv.Itoa(num)).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	waitForOutputEquals(oc, ns, "deployment/"+deployName, "{.status.availableReplicas}", expReplicas)
	podList, err := compat_otp.GetAllPodsWithLabel(oc, ns, "name="+deployName)
	o.Expect(err).NotTo(o.HaveOccurred())
	return podList
}

// GetPrometheusMetrics queries a specified metrics from Prometheus
func getPrometheusMetrics(oc *exutil.CLI, token, query string) (string, error) {
	var (
		promURL = "https://prometheus-k8s.openshift-monitoring.svc:9091/api/v1/query"
	)

	e2e.Logf("Querying Prometheus for metric: %s", query)
	output, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args(
		"-n", "openshift-monitoring",
		"-c", "prometheus",
		"prometheus-k8s-0",
		"--",
		"curl", "-k", "-s",
		"-H", fmt.Sprintf("Authorization: Bearer %s", token),
		fmt.Sprintf("%s?query=%s", promURL, query),
		"--connect-timeout", "10",
		"--max-time", "30",
	).Output()

	if err != nil {
		return "", fmt.Errorf("failed to query Prometheus: %v", err)
	}

	if len(output) > 10000 {
		e2e.Logf("Prometheus query response (truncated): %s...", output[:10000])
	} else {
		e2e.Logf("Prometheus query response: %s", output)
	}

	if !strings.Contains(output, `"status":"success"`) {
		return "", fmt.Errorf("Prometheus query did not return success status: %s", output)
	}

	return output, nil
}

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
	compat_otp.AssertWaitPollNoErr(err, fmt.Sprintf("fail to process %v", parameters))
	e2e.Logf("the file of resource is %s", jsonCfg)
	return jsonCfg
}
