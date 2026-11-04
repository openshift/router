package util

import (
	"bufio"
	"context"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	configclientv1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	exutil "github.com/openshift/origin/test/extended/util"
	compat_otp "github.com/openshift/origin/test/extended/util/compat_otp"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	e2e "k8s.io/kubernetes/test/e2e/framework"
)

type IngressControllerDescription struct {
	Name        string
	Namespace   string
	DefaultCert string
	Domain      string
	Shard       string
	Replicas    int
	Template    string
}

// IngressControllerHostPortDescription describes a host-network ingress controller with custom ports.
type IngressControllerHostPortDescription struct {
	Name        string
	Namespace   string
	DefaultCert string
	Domain      string
	HTTPPort    int
	HTTPSPort   int
	StatsPort   int
	Replicas    int
	Template    string
}

type RouteDescription struct {
	Name      string
	Namespace string
	Domain    string
	SubDomain string
	Template  string
}

type WebServerDeployDescription struct {
	DeployName      string
	SvcSecureName   string
	SvcUnsecureName string
	Template        string
	Namespace       string
}

func (ingctrl *IngressControllerDescription) Create(oc *exutil.CLI) {
	availableWorkerNode, _ := ExactNodeDetails(oc)
	if availableWorkerNode < 1 {
		g.Skip("Skipping as there is no enough worker nodes")
	}
	err := CreateResourceFromTemplate(oc, "--ignore-unknown-parameters=true", "-f", ingctrl.Template, "-p", "NAME="+ingctrl.Name, "NAMESPACE="+ingctrl.Namespace, "DOMAIN="+ingctrl.Domain, "SHARD="+ingctrl.Shard)
	o.Expect(err).NotTo(o.HaveOccurred())
}

func (ingctrl *IngressControllerDescription) Delete(oc *exutil.CLI) error {
	return oc.AsAdmin().WithoutNamespace().Run("delete").Args("--ignore-not-found", "-n", ingctrl.Namespace, "ingresscontroller", ingctrl.Name).Execute()
}

// Create creates a host-network ingress controller using its template and configured ports.
func (ingctrl *IngressControllerHostPortDescription) Create(oc *exutil.CLI) {
	err := CreateResourceFromTemplate(oc, "--ignore-unknown-parameters=true", "-f", ingctrl.Template,
		"-p", "NAME="+ingctrl.Name, "NAMESPACE="+ingctrl.Namespace, "DOMAIN="+ingctrl.Domain,
		"HTTPPORT="+strconv.Itoa(ingctrl.HTTPPort), "HTTPSPORT="+strconv.Itoa(ingctrl.HTTPSPort), "STATSPORT="+strconv.Itoa(ingctrl.StatsPort))
	o.Expect(err).NotTo(o.HaveOccurred())
}

// Delete deletes a host-network ingress controller, ignoring a missing resource.
func (ingctrl *IngressControllerHostPortDescription) Delete(oc *exutil.CLI) error {
	return oc.AsAdmin().WithoutNamespace().Run("delete").Args("--ignore-not-found", "-n", ingctrl.Namespace, "ingresscontroller", ingctrl.Name).Execute()
}

// Create route object from template.
func (route *RouteDescription) Create(oc *exutil.CLI) {
	err := CreateResourceToNsFromTemplate(oc, route.Namespace, "--ignore-unknown-parameters=true", "-f", route.Template, "-p", "SUBDOMAIN_NAME="+route.SubDomain, "NAMESPACE="+route.Namespace, "DOMAIN="+route.Domain)
	o.Expect(err).NotTo(o.HaveOccurred())
}

// Create web server deployment from template
func (websrvdeploy *WebServerDeployDescription) Create(oc *exutil.CLI) {
	// Mirror before processing: the image reference is a literal in the template
	// body, not a template parameter.
	template := MirrorFixtureImages(oc, websrvdeploy.Namespace, websrvdeploy.Template)
	err := CreateResourceToNsFromTemplate(oc, websrvdeploy.Namespace, "--ignore-unknown-parameters=true", "-f", template, "-p", "DEPLOY_NAME="+websrvdeploy.DeployName, "SVC_SECURE_NAME="+websrvdeploy.SvcSecureName, "SVC_UNSECURE_NAME="+websrvdeploy.SvcUnsecureName)
	o.Expect(err).NotTo(o.HaveOccurred())
}

func (websrvdeploy *WebServerDeployDescription) Delete(oc *exutil.CLI) error {
	return oc.AsAdmin().WithoutNamespace().Run("delete").Args("-n", websrvdeploy.Namespace, "deployment", websrvdeploy.DeployName).Execute()
}

func GetRandomString() string {
	chars := "abcdefghijklmnopqrstuvwxyz0123456789"
	seed := rand.New(rand.NewSource(time.Now().UnixNano()))
	buffer := make([]byte, 8)
	for index := range buffer {
		buffer[index] = chars[seed.Intn(len(chars))]
	}
	return string(buffer)
}

func GetBaseDomain(oc *exutil.CLI) string {
	var basedomain string

	basedomain, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("dns.config/cluster", "-o=jsonpath={.spec.baseDomain}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("the base domain of the cluster: %v", basedomain)
	return basedomain
}

func GetSAToken(oc *exutil.CLI, sa, ns string) (string, error) {
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

func WaitForCustomIngressControllerAvailable(oc *exutil.CLI, icname string) error {
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

func EnsureCustomIngressControllerAvailable(oc *exutil.CLI, icName string) {
	ns := "openshift-ingress-operator"
	err := WaitForCustomIngressControllerAvailable(oc, icName)
	// print custom ingresscontroller description for debugging purpose if err
	if err != nil {
		output, _ := oc.AsAdmin().WithoutNamespace().Run("describe").Args("-n", ns, "ingresscontroller", icName).Output()
		e2e.Logf("The description of ingresscontroller %v is:\n%v", icName, output)
	}
	compat_otp.AssertWaitPollNoErr(err, fmt.Sprintf("max time reached but ingresscontroller %v is not available", icName))
}

func EnsureRouteIsAdmittedByIngressController(oc *exutil.CLI, ns, routeName, icName string) {
	jsonPath := fmt.Sprintf(`{.status.ingress[?(@.routerName=="%s")].conditions[?(@.type=="Admitted")].status}`, icName)
	WaitForOutputEquals(oc, ns, "route/"+routeName, jsonPath, "True")
}

func GetOnePodNameByLabel(oc *exutil.CLI, ns, label string) string {
	podName, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("pods", "-l", label, "-o=jsonpath={.items[0].metadata.name}", "-n", ns).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("the one pod with label %v is %v", label, podName)
	return podName
}

// GetOneNewRouterPodFromRollingUpdate immediately after/during deployment rolling update, don't care the previous pod status
func GetOneNewRouterPodFromRollingUpdate(oc *exutil.CLI, icName string) string {
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
	err := WaitForPodWithLabelReady(oc, ns, rsLabel)
	if err != nil {
		output, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("pods", "-n", ns).Output()
		e2e.Logf("All current router pods are:\n%v", output)
	}
	compat_otp.AssertWaitPollNoErr(err, "the new router pod failed to be ready within allowed time!")
	return GetOnePodNameByLabel(oc, ns, rsLabel)
}

func EnsureRouterDeployGenerationIs(oc *exutil.CLI, icName, expectGeneration string) {
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

func WaitForPodWithLabelReady(oc *exutil.CLI, ns, label string) error {
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

func EnsurePodWithLabelReady(oc *exutil.CLI, ns, label string) {
	err := WaitForPodWithLabelReady(oc, ns, label)
	// print pod status and logs for debugging purpose if err
	if err != nil {
		output, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("pod", "-n", ns, "-l", label).Output()
		e2e.Logf("All pods with label %v are:\n%v", label, output)
		logs, _ := oc.AsAdmin().WithoutNamespace().Run("logs").Args("-n", ns, "-l", label, "--tail=10").Output()
		e2e.Logf("The logs of all labeled pods are:\n%v", logs)
	}
	compat_otp.AssertWaitPollNoErr(err, fmt.Sprintf("max time reached but the pods with label %v are not ready", label))
}

// WaitForResourceToDisappear waits for the named resource to disappear, e.g. used while router deployment rolled out
func WaitForResourceToDisappear(oc *exutil.CLI, ns, rsname string) error {
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

// internalRegistryHostname is the in-cluster address of the integrated image
// registry.  A pod that references an ImageStream tag through this address pulls
// the image from the cluster instead of reaching out to the public internet.
const internalRegistryHostname = "image-registry.openshift-image-registry.svc:5000"

// fixtureImageRe matches the external fixture images that the manifests under
// testdata are written against, capturing the repository's final path segment and
// the digest.
var fixtureImageRe = regexp.MustCompile(`quay\.io/openshifttest/([a-zA-Z0-9._-]+)@sha256:([0-9a-f]{64})`)

// MirrorFixtureImages imports every external fixture image referenced by the
// given manifest into an ImageStream in ns, and returns the path of an equivalent
// manifest that pulls those images from the cluster's integrated registry.  A
// manifest that references no such image is returned unchanged.
//
// Referencing an external registry from a pod spec fails in CI two different
// ways.  The known-image-checker monitor flags any image that is not in the
// mirrored registry and fails the job.  Separately, on a cluster with no route to
// quay.io the pod simply never becomes ready, so the test times out part way
// through an assertion and reports a misleading failure instead of the real
// cause; this is what happened to the ingress operator tests on disconnected IPv6
// metal lanes in OCPBUGS-132070.  Importing through an ImageStream addresses the
// first problem outright, and turns the second into a single explicit step whose
// failure message names the image that could not be fetched.
//
// Note that the import still reads from the source registry, so this does not by
// itself make the fixtures work on a fully disconnected cluster.  It makes the
// dependency explicit and fails fast when it cannot be satisfied.
func MirrorFixtureImages(oc *exutil.CLI, ns, file string) string {
	manifest, err := ioutil.ReadFile(file)
	o.Expect(err).NotTo(o.HaveOccurred(), "failed to read fixture %s", file)

	matches := fixtureImageRe.FindAllStringSubmatch(string(manifest), -1)
	if len(matches) == 0 {
		return file
	}

	skipWithoutInternalRegistry(oc)

	rewritten := string(manifest)
	mirrored := make(map[string]bool, len(matches))
	for _, match := range matches {
		source, name, digest := match[0], match[1], match[2]
		if mirrored[source] {
			continue
		}
		mirrored[source] = true
		// Tag with a prefix of the digest so that two manifests pinning different
		// digests of the same repository get distinct tags rather than colliding.
		tag := digest[:12]
		importImageStreamTag(oc, ns, name, tag, source)
		rewritten = strings.ReplaceAll(rewritten, source,
			fmt.Sprintf("%s/%s/%s:%s", internalRegistryHostname, ns, name, tag))
	}

	mirroredFile := filepath.Join(os.TempDir(), GetRandomString()+"-"+filepath.Base(file))
	err = ioutil.WriteFile(mirroredFile, []byte(rewritten), 0644)
	o.Expect(err).NotTo(o.HaveOccurred(), "failed to write mirrored fixture %s", mirroredFile)
	g.DeferCleanup(os.Remove, mirroredFile)
	e2e.Logf("mirrored fixture %s to %s for namespace %s", file, mirroredFile, ns)
	return mirroredFile
}

// skipWithoutInternalRegistry skips only when registry configuration explicitly
// removes the registry or ClusterVersion confirms that its capability is disabled.
// Lookup failures must fail the spec rather than hide cluster or RBAC problems.
func skipWithoutInternalRegistry(oc *exutil.CLI) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	reason, err := internalRegistrySkipReason(ctx,
		oc.AdminDynamicClient().Resource(imageregistryv1.GroupVersion.WithResource("configs")),
		oc.AdminConfigClient().ConfigV1().ClusterVersions())
	o.Expect(err).NotTo(o.HaveOccurred(), "failed to determine whether the integrated image registry is available")
	if reason != "" {
		g.Skip(reason)
	}
}

// internalRegistrySkipReason retries transient lookup errors and returns a skip
// reason only for a confirmed unsupported registry configuration.
func internalRegistrySkipReason(ctx context.Context, registry dynamic.ResourceInterface, versions configclientv1.ClusterVersionInterface) (string, error) {
	var reason string
	var lastErr error
	err := wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		config, err := registry.Get(ctx, "cluster", metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			// A missing config can also mean that an enabled registry is broken.
			// Confirm that the optional capability is disabled before skipping.
			version, versionErr := versions.Get(ctx, "version", metav1.GetOptions{})
			if versionErr != nil {
				err = fmt.Errorf("checking ImageRegistry capability: %w", versionErr)
			} else if slices.Contains(version.Status.Capabilities.KnownCapabilities, configv1.ClusterVersionCapabilityImageRegistry) &&
				!slices.Contains(version.Status.Capabilities.EnabledCapabilities, configv1.ClusterVersionCapabilityImageRegistry) {
				reason = "the ImageRegistry capability is disabled, so fixture images cannot be mirrored into the cluster"
				return true, nil
			}
		}
		if err != nil {
			lastErr = err
			if isTransientRegistryError(err) {
				e2e.Logf("retrying integrated registry lookup after transient error: %v", err)
				return false, nil
			}
			return false, err
		}
		state, found, err := unstructured.NestedString(config.Object, "spec", "managementState")
		if err != nil {
			return false, fmt.Errorf("reading registry config/cluster spec.managementState: %w", err)
		}
		if !found || strings.TrimSpace(state) == "" {
			return false, fmt.Errorf("registry config/cluster has no spec.managementState")
		}
		if strings.EqualFold(strings.TrimSpace(state), "Removed") {
			reason = "the image registry operator is Removed, so fixture images cannot be mirrored into the cluster"
		}
		return true, nil
	})
	if err != nil {
		return "", fmt.Errorf("integrated registry lookup failed: %w; last API error: %v", err, lastErr)
	}
	return reason, nil
}

// isTransientRegistryError identifies API availability and transport errors that
// can recover within the registry lookup's bounded context.
func isTransientRegistryError(err error) bool {
	if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
		return false
	}
	return apierrors.IsTimeout(err) || apierrors.IsServerTimeout(err) ||
		apierrors.IsServiceUnavailable(err) || apierrors.IsTooManyRequests(err) ||
		apierrors.IsInternalError(err) || utilnet.IsTimeout(err) ||
		utilnet.IsConnectionReset(err) || utilnet.IsConnectionRefused(err) ||
		utilnet.IsProbableEOF(err)
}

// importImageStreamTag imports source into the ns/name:tag ImageStream tag.  The
// import is retried, because pulling from a registry is prone to transient
// failures such as throttling or an integrated registry that is not yet serving,
// and the OTE guidelines require tests to retry through those rather than fail.
//
// PreserveOriginal keeps a multi-architecture manifest list intact, so that the
// same ImageStream tag works on every architecture the tests run on.
func importImageStreamTag(oc *exutil.CLI, ns, name, tag, source string) {
	var lastOutput string
	waitErr := wait.PollImmediate(5*time.Second, 3*time.Minute, func() (bool, error) {
		output, err := oc.AsAdmin().WithoutNamespace().Run("import-image").Args(
			name+":"+tag,
			"--from="+source,
			"--confirm",
			"--reference-policy=local",
			"--import-mode=PreserveOriginal",
			"-n", ns,
		).Output()
		if err != nil {
			lastOutput = output
			e2e.Logf("waiting to import %s into imagestream %s/%s:%s: %v", source, ns, name, tag, err)
			return false, nil
		}
		return true, nil
	})
	compat_otp.AssertWaitPollNoErr(waitErr, fmt.Sprintf(
		"failed to import %s into imagestream %s/%s:%s, last output: %s", source, ns, name, tag, lastOutput))
}

// CreateResourceFromFile mirrors fixture images and creates resources from a
// manifest in the specified namespace.
func CreateResourceFromFile(oc *exutil.CLI, ns, file string) {
	err := oc.WithoutNamespace().Run("create").Args("-f", MirrorFixtureImages(oc, ns, file), "-n", ns).Execute()
	o.Expect(err).NotTo(o.HaveOccurred())
}

// CreateResourceFromWebServer creates resources from files like web-server-rc.yaml and web-server-signed-deploy.yaml
func CreateResourceFromWebServer(oc *exutil.CLI, ns, file, srvrcInfo string) []string {
	CreateResourceFromFile(oc, ns, file)
	err := WaitForPodWithLabelReady(oc, ns, "name="+srvrcInfo)
	compat_otp.AssertWaitPollNoErr(err, "backend server pod failed to be ready state within allowed time!")
	srvPodList := GetPodListByLabel(oc, ns, "name="+srvrcInfo)
	return srvPodList
}

// OperateResourceFromFile creates or deletes resources in the specified namespace from the file (not template)
func OperateResourceFromFile(oc *exutil.CLI, oper, ns, file string) {
	// Only an operation that admits the resource needs its images mirrored; a
	// delete matches on name alone, and importing images for it would be wasted
	// work against the source registry.
	if oper != "delete" {
		file = MirrorFixtureImages(oc, ns, file)
	}
	err := oc.AsAdmin().WithoutNamespace().Run(oper).Args("-f", file, "-n", ns).Execute()
	o.Expect(err).NotTo(o.HaveOccurred())
}

// PatchResourceAsAdmin patches a resource in the specified namespace with 'merge' type
func PatchResourceAsAdmin(oc *exutil.CLI, ns, resource, patch string) {
	PatchResourceAsAdminAnyType(oc, ns, resource, patch, "merge")
}

// PatchResourceAsAdminAnyType patches a resource in the specified namespace with any type
func PatchResourceAsAdminAnyType(oc *exutil.CLI, ns, resource, patch, typ string) {
	err := oc.AsAdmin().WithoutNamespace().Run("patch").Args(resource, "-p", patch, "--type="+typ, "-n", ns).Execute()
	o.Expect(err).NotTo(o.HaveOccurred())
}

func CreateRoute(oc *exutil.CLI, ns, routeType, routeName, serviceName string, extraParas []string) {
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

func SetAnnotation(oc *exutil.CLI, ns, resource, annotation string) {
	err := oc.Run("annotate").Args("-n", ns, resource, annotation, "--overwrite").Execute()
	o.Expect(err).NotTo(o.HaveOccurred())
}

// SetAnnotationAsAdmin sets an annotation on a resource using an administrator client.
func SetAnnotationAsAdmin(oc *exutil.CLI, ns, resource, annotation string) {
	err := oc.AsAdmin().WithoutNamespace().Run("annotate").Args("-n", ns, resource, annotation, "--overwrite").Execute()
	o.Expect(err).NotTo(o.HaveOccurred())
}

// GetAnnotation reads the annotation from the given resource
func GetAnnotation(oc *exutil.CLI, ns, resource, resourceName string) string {
	findAnnotation, err := oc.AsAdmin().WithoutNamespace().Run("get").Args(
		resource, resourceName, "-n", ns, "-o=jsonpath={.metadata.annotations}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	return findAnnotation
}

// GetByJsonPath collects resource values with jsonpath option
func GetByJsonPath(oc *exutil.CLI, ns, resource, jsonPath string) string {
	output, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("-n", ns, resource, "-o=jsonpath="+jsonPath).Output()
	if err != nil {
		e2e.Logf("the error is: %v", err.Error())
	}
	e2e.Logf("the output filtered by jsonpath is: %v", output)
	return output
}

// GetByLabelAndJsonPath gets resource using label and filtered by jsonpath
func GetByLabelAndJsonPath(oc *exutil.CLI, ns, resource, label, jsonPath string) string {
	output, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("-n", ns, resource, "-l", label, "-ojsonpath="+jsonPath).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("the output filtered by label and jsonpath is: %v", output)
	return output
}

// GetOneRouterPodNameByIC collects a single pod name for general use.
func GetOneRouterPodNameByIC(oc *exutil.CLI, icname string) string {
	podName, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("pods", "-l", "ingresscontroller.operator.openshift.io/deployment-ingresscontroller="+icname, "-o=jsonpath={.items[0].metadata.name}", "-n", "openshift-ingress").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("the result of router pod name: %v", podName)
	return podName
}

// EnsureHaproxyBlockConfigContains checks the route data is present in the haproxy.config
func EnsureHaproxyBlockConfigContains(oc *exutil.CLI, routerPodName string, blockCfgStart string, searchList []string) string {
	var (
		haproxyCfg string
		j          = 0
	)

	e2e.Logf("Polling and search haproxy config file")
	waitErr := wait.Poll(5*time.Second, 60*time.Second, func() (bool, error) {
		haproxyCfg = GetBlockConfig(oc, routerPodName, blockCfgStart)
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

// EnsureHaproxyBlockConfigNotContains checks the route data is not present in the haproxy.config
func EnsureHaproxyBlockConfigNotContains(oc *exutil.CLI, routerPodName string, blockCfgStart string, searchList []string) string {
	var (
		haproxyCfg string
		j          = 0
	)

	e2e.Logf("Polling and search haproxy config file")
	waitErr := wait.Poll(5*time.Second, 30*time.Second, func() (bool, error) {
		haproxyCfg = GetBlockConfig(oc, routerPodName, blockCfgStart)
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

// GetBlockConfig gets block content of haproxy.conf.
func GetBlockConfig(oc *exutil.CLI, routerPodName, searchString string) string {
	output, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", routerPodName, "--", "bash", "-c", "cat haproxy.config").Output()
	o.Expect(err).NotTo(o.HaveOccurred(), "get the content of haproxy.config failed")
	// Scope route block searches to the test namespace so that, for example,
	// "route-edge" does not match "route-edge15873" from another test running
	// in parallel. Global directives such as "bind :80 accept-proxy" must not
	// be filtered because they never contain a namespace.
	ns := oc.Namespace()
	result := ""
	flag := 0
	startIndex := 0
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, searchString) {
			trimmed := strings.TrimSpace(line)
			isRouteBlock := strings.HasPrefix(trimmed, "backend ") ||
				strings.HasPrefix(trimmed, "listen ") ||
				strings.HasPrefix(trimmed, "frontend ")
			if flag == 0 && ns != "" && isRouteBlock && !strings.Contains(searchString, ns) && !strings.Contains(line, ns) {
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

// ReadRouterPodData collects information from router pod.
func ReadRouterPodData(oc *exutil.CLI, routername, executeCmd string, searchString string) string {
	output := ReadPodData(oc, routername, "openshift-ingress", executeCmd, searchString)
	return output
}

// GetIngressctlDomain collects ingresscontroller domain name
func GetIngressctlDomain(oc *exutil.CLI, icname string) string {
	var ingressctldomain string
	ingressctldomain, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("ingresscontroller", icname, "--namespace=openshift-ingress-operator", "-o=jsonpath={.status.domain}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("the domain for the ingresscontroller is : %v", ingressctldomain)
	return ingressctldomain
}

// GetPodv4Address gets the ipv4 address of the given pod
func GetPodv4Address(oc *exutil.CLI, podName, namespace string) string {
	podIPv4, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("pod", podName, "-n", namespace, "-o=jsonpath={.status.podIP}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("IP of the %s pod in namespace %s is %q ", podName, namespace, podIPv4)
	return podIPv4
}

// GetPodListByLabel obtains the pod name based on the particular label
func GetPodListByLabel(oc *exutil.CLI, namespace string, label string) []string {
	var podList []string
	podNameAll, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("-n", namespace, "pod", "-l", label, "-ojsonpath={.items..metadata.name}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	podList = strings.Split(podNameAll, " ")
	e2e.Logf("The pod list is %v", podList)
	return podList
}

// EnsureClusterOperatorNormal waits for the cluster operator back to normal status ("True False False")
func EnsureClusterOperatorNormal(oc *exutil.CLI, coName string, healthyThreshold int, totalWaitTime int) {
	count := 0
	printCount := 0
	jsonPath := `{.status.conditions[?(@.type=="Available")].status}{.status.conditions[?(@.type=="Progressing")].status}{.status.conditions[?(@.type=="Degraded")].status}`

	e2e.Logf("waiting for CO %v back to normal status......", coName)
	waitErr := wait.PollImmediate(5*time.Second, time.Duration(totalWaitTime)*time.Second, func() (bool, error) {
		status := GetByJsonPath(oc, "default", "co/"+coName, jsonPath)
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
		output := GetByJsonPath(oc, "default", "co/"+coName, "{.status.conditions}")
		e2e.Logf("The co %v is abnormal and here is status: %v", coName, output)
		if coName == "ingress" {
			output, _ = oc.AsAdmin().WithoutNamespace().Run("describe").Args("-n", "openshift-ingress", "service", "router-default").Output()
			e2e.Logf("The output of describe router-default service: %v", output)
		}
	}
	compat_otp.AssertWaitPollNoErr(waitErr, fmt.Sprintf("reached max time allowed but CO %v is still abnoraml.", coName))
}

// ReadPodData searches the specific data from the given pod
func ReadPodData(oc *exutil.CLI, podname string, ns string, executeCmd string, searchString string) string {
	cmd := fmt.Sprintf("%s | grep \"%s\"", executeCmd, searchString)
	output, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", ns, podname, "--", "bash", "-c", cmd).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("the matching part is: %s", output)
	return output
}

// WaitForOutsideCurlContains waits until curling a route returns expected output.
func WaitForOutsideCurlContains(url string, curlOptions string, expected string) string {
	return waitForOutsideCurl(url, curlOptions, expected, func(result string) bool {
		return strings.Contains(result, expected)
	})
}

// WaitForOutsideCurlRedirect waits until a HEAD request returns an HTTP 302
// response with the expected Location value. Header names are case-insensitive,
// including when a proxy changes their capitalization.
func WaitForOutsideCurlRedirect(url, expectedLocation string) string {
	return waitForOutsideCurl(url, "-I -k", "HTTP 302 redirect to "+expectedLocation, func(result string) bool {
		return matchesHTTPRedirect(result, expectedLocation)
	})
}

// matchesHTTPRedirect reports whether output contains an HTTP 302 response with
// the exact expected Location value. Only header names are case-insensitive;
// URL paths retain their case-sensitive semantics.
func matchesHTTPRedirect(output, expectedLocation string) bool {
	response, err := http.ReadResponse(bufio.NewReader(strings.NewReader(output)), &http.Request{Method: http.MethodHead})
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode == http.StatusFound && response.Header.Get("Location") == expectedLocation
}

// waitForOutsideCurl polls curl until its output matches or an expected
// execution error occurs, and logs a verbose response on timeout.
func waitForOutsideCurl(url, curlOptions, expected string, matches func(string) bool) string {
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
		if !matches(string(result)) {
			e2e.Logf("curl response did not match %q: %s, retrying...", expected, result)
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
	compat_otp.AssertWaitPollNoErr(waitErr, fmt.Sprintf("max time reached waiting for %q from %s", expected, url))
	return string(output)
}

// WaitForCurl polls curl command
func WaitForCurl(oc *exutil.CLI, podName, baseDomain string, routestring string, searchWord string, controllerIP string) {
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

// GetRouteHost gets the route hostname
func GetRouteHost(oc *exutil.CLI, ns, routeName string) string {
	host, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("route", routeName, "-n", ns, `-ojsonpath={.spec.host}`).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("the host of the route %v is %v.", routeName, host)
	return host
}

// GetRoutes gets the route detail
func GetRoutes(oc *exutil.CLI, ns string) string {
	output, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("route", "-n", ns).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("oc get route: %v", output)
	return output
}

// GetResourceName obtains the resource name like ingress's, route's name
func GetResourceName(oc *exutil.CLI, namespace, resourceName string) []string {
	var resourceList []string
	resourceNames, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("-n", namespace, resourceName,
		"-ojsonpath={.items..metadata.name}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	resourceList = strings.Split(resourceNames, " ")
	e2e.Logf("The resource '%s' names are  %v ", resourceName, resourceList)
	return resourceList
}

// CheckProxy checks whether proxy is configured or not
func CheckProxy(oc *exutil.CLI) bool {
	httpProxy, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("proxy", "cluster", "-o=jsonpath={.status.httpProxy}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	httpsProxy, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("proxy", "cluster", "-o=jsonpath={.status.httpsProxy}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	if httpProxy != "" || httpsProxy != "" {
		return true
	}
	return false
}

// CheckRouteDetailsRemoved retrieves the status of the route after using RouteSelectors
func CheckRouteDetailsRemoved(oc *exutil.CLI, namespace, routeName, ingresscontrollerName string) {
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

// RepeatCmdOnClient executes a command on the internal or external client for the desired times
func RepeatCmdOnClient(oc *exutil.CLI, cmd, expectOutput interface{}, duration int, repeatTimes int) (string, []int) {
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

	waitErr := wait.Poll(1*time.Second, time.Duration(duration)*time.Second, func() (bool, error) {
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

// CheckGivenStringPresentOrNot checks whether given string is present or not in a list
func CheckGivenStringPresentOrNot(shouldContain bool, iterateObject []string, searchString string) {
	if shouldContain {
		o.Expect(iterateObject).To(o.ContainElement(o.ContainSubstring(searchString)))
	} else {
		o.Expect(iterateObject).NotTo(o.ContainElement(o.ContainSubstring(searchString)))
	}
}

// WaitForOutputContains polls to check output which should contain the expected string
func WaitForOutputContains(oc *exutil.CLI, ns, resourceName, jsonPath, expected string, args ...interface{}) {
	waitDuration := 180 * time.Second
	for _, arg := range args {
		duration, ok := arg.(time.Duration)
		if ok {
			waitDuration = duration
		}
	}

	waitErr := wait.PollImmediate(5*time.Second, waitDuration, func() (bool, error) {
		output := GetByJsonPath(oc, ns, resourceName, jsonPath)
		if strings.Contains(output, expected) {
			return true, nil
		}
		e2e.Logf("The output of jsonpath does NOT contain the expected string: %v, retrying...", expected)
		return false, nil
	})
	compat_otp.AssertWaitPollNoErr(waitErr, fmt.Sprintf("max time reached but cannot find the expected string"))
}

// WaitForOutputEquals polls to check output which should equal the expected string
func WaitForOutputEquals(oc *exutil.CLI, ns, resourceName, jsonPath, expected string, args ...interface{}) {
	waitDuration := 180 * time.Second
	for _, arg := range args {
		duration, ok := arg.(time.Duration)
		if ok {
			waitDuration = duration
		}
	}

	waitErr := wait.PollImmediate(5*time.Second, waitDuration, func() (bool, error) {
		output := GetByJsonPath(oc, ns, resourceName, jsonPath)
		if output == expected {
			return true, nil
		}
		e2e.Logf("The output of jsonpath does NOT equal the expected string: %v, retrying...", expected)
		return false, nil
	})
	compat_otp.AssertWaitPollNoErr(waitErr, fmt.Sprintf("max time reached but cannot find the expected string"))
}

// WaitForOutputMatchRegexp keeps checking until the searching for the regular expression matches
func WaitForOutputMatchRegexp(oc *exutil.CLI, ns, resourceName, jsonPath, regExpress string, args ...interface{}) string {
	result := "NotMatch"
	waitDuration := 180 * time.Second
	for _, arg := range args {
		duration, ok := arg.(time.Duration)
		if ok {
			waitDuration = duration
		}
	}

	waitErr := wait.Poll(5*time.Second, waitDuration, func() (bool, error) {
		sourceRange := GetByJsonPath(oc, ns, resourceName, jsonPath)
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

// SearchInDescribeResource searches in the polled and described resource details
func SearchInDescribeResource(oc *exutil.CLI, resource, resourceName, match string) string {
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

// CheckCookieFile checks the cookie file generated through curl command.
//
// curl writes the cookie jar when the transfer finishes, and if it cannot create
// the file it says so only as a warning on stderr -- the exit status stays zero.
// A bare read therefore fails later with a plain "no such file or directory"
// that says nothing about why, so poll briefly for the file and, when it never
// appears, report what is actually in the directory.
func CheckCookieFile(fileDir string, expectedString string) {
	var content string
	waitErr := wait.PollImmediate(2*time.Second, 30*time.Second, func() (bool, error) {
		output, err := ioutil.ReadFile(fileDir)
		if err != nil {
			e2e.Logf("waiting for the cookie file %s: %v", fileDir, err)
			return false, nil
		}
		content = string(output)
		return strings.Contains(content, expectedString), nil
	})
	if waitErr != nil && content == "" {
		entries, err := ioutil.ReadDir(filepath.Dir(fileDir))
		if err != nil {
			e2e.Logf("the cookie directory %s cannot be read: %v", filepath.Dir(fileDir), err)
		} else {
			names := make([]string, 0, len(entries))
			for _, entry := range entries {
				names = append(names, entry.Name())
			}
			e2e.Logf("the cookie directory %s contains: %v", filepath.Dir(fileDir), names)
		}
	}
	e2e.Logf("the cookie file content is: %s", content)
	compat_otp.AssertWaitPollNoErr(waitErr, fmt.Sprintf(
		"the cookie file %s never contained %q; curl reports a jar it cannot write only as a warning, "+
			"so check the curl output above for \"failed to save cookies\"", fileDir, expectedString))
}

// WaitForCurlInPodContains runs curlCmd inside podName until the command
// succeeds and its output contains expected, and returns that output.
//
// The command reaches the pod over an exec stream to the API server, and that
// stream is reset often enough ("error reading from error stream ... connection
// reset by peer") that a single attempt makes the test report a routing failure
// when nothing was wrong with the route.  The last output and error are kept so
// that a genuine failure is still diagnosable.
func WaitForCurlInPodContains(oc *exutil.CLI, ns, podName, curlCmd, expected string) string {
	var output string
	var lastErr error
	waitErr := wait.PollImmediate(5*time.Second, 90*time.Second, func() (bool, error) {
		output, lastErr = compat_otp.RemoteShPod(oc, ns, podName, "sh", "-c", curlCmd)
		if lastErr != nil {
			e2e.Logf("the curl command in pod %s failed: %v, retrying...", podName, lastErr)
			return false, nil
		}
		if !strings.Contains(output, expected) {
			e2e.Logf("cannot find %q in the curl output, retrying...", expected)
			return false, nil
		}
		return true, nil
	})
	compat_otp.AssertWaitPollNoErr(waitErr, fmt.Sprintf(
		"the curl command %q in pod %s never returned %q; last error: %v, last output: %s",
		curlCmd, podName, expected, lastErr, output))
	return output
}

func UpdateFilebySedCmd(file, toBeReplaced, newContent string) {
	sedCmd := fmt.Sprintf(`sed -i'' -e 's|%s|%s|g' %s`, toBeReplaced, newContent, file)
	_, err := exec.Command("bash", "-c", sedCmd).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
}

// ScaleDeploy scales Deployment for DCM testing
func ScaleDeploy(oc *exutil.CLI, ns, deployName string, num int) []string {
	expReplicas := strconv.Itoa(num)
	if num == 0 {
		expReplicas = ""
	}
	_, err := oc.AsAdmin().WithoutNamespace().Run("scale").Args("-n", ns, "deployment/"+deployName, "--replicas="+strconv.Itoa(num)).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	WaitForOutputEquals(oc, ns, "deployment/"+deployName, "{.status.availableReplicas}", expReplicas)
	podList, err := compat_otp.GetAllPodsWithLabel(oc, ns, "name="+deployName)
	o.Expect(err).NotTo(o.HaveOccurred())
	return podList
}

// GetPrometheusMetrics queries a specified metrics from Prometheus
func GetPrometheusMetrics(oc *exutil.CLI, token, query string) (string, error) {
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

func ExactNodeDetails(oc *exutil.CLI) (int, string) {
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

func CreateResourceFromTemplate(oc *exutil.CLI, parameters ...string) error {
	jsonCfg := ParseToJSON(oc, parameters)
	return oc.AsAdmin().WithoutNamespace().Run("create").Args("-f", jsonCfg).Execute()
}

func CreateResourceToNsFromTemplate(oc *exutil.CLI, ns string, parameters ...string) error {
	jsonCfg := ParseToJSON(oc, parameters)
	return oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", ns, "-f", jsonCfg).Execute()
}

func CreateUserResourceToNsFromTemplate(oc *exutil.CLI, ns string, parameters ...string) (string, error) {
	jsonCfg := ParseToJSON(oc, parameters)
	return oc.WithoutNamespace().Run("create").Args("-n", ns, "-f", jsonCfg).Output()
}

func ParseToJSON(oc *exutil.CLI, parameters []string) string {
	var jsonCfg string
	err := wait.Poll(3*time.Second, 15*time.Second, func() (bool, error) {
		output, err := oc.AsAdmin().Run("process").Args(parameters...).OutputToFile(GetRandomString() + "-temp-resource.json")
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

// AddExtraParametersToYamlFile writes a copy of a YAML file with content inserted after matching lines.
func AddExtraParametersToYamlFile(originalFile, flagPara, addedContent string) string {
	return addContentToYamlFile(originalFile, flagPara, addedContent, 0)
}

// AddContentToFileWithMatchedOrder inserts content after the selected matching line in a file.
func AddContentToFileWithMatchedOrder(originalFile, flagPara, addedContent string, sequence int) string {
	return addContentToYamlFile(originalFile, flagPara, addedContent, sequence)
}

func addContentToYamlFile(originalFile, flagPara, addedContent string, sequence int) string {
	content, err := ioutil.ReadFile(originalFile)
	o.Expect(err).NotTo(o.HaveOccurred(), "failed to read input fixture")
	var updated strings.Builder
	matched := 0
	for _, line := range strings.Split(string(content), "\n") {
		updated.WriteString(line)
		updated.WriteByte('\n')
		if strings.Contains(line, flagPara) {
			matched++
			if sequence == 0 || sequence == matched {
				updated.WriteString(addedContent)
			}
		}
	}
	newFile := filepath.Join(filepath.Dir(originalFile), GetRandomString()+filepath.Ext(originalFile))
	err = ioutil.WriteFile(newFile, []byte(updated.String()), 0644)
	o.Expect(err).NotTo(o.HaveOccurred(), "failed to write updated fixture")
	return newFile
}

// CreateConfigMapFromFile creates a ConfigMap from a local file.
func CreateConfigMapFromFile(oc *exutil.CLI, ns, name, file string) {
	_, err := oc.AsAdmin().WithoutNamespace().Run("create").Args("configmap", name, "--from-file="+file, "-n", ns).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
}

// DeleteConfigMap deletes a ConfigMap from the given namespace.
func DeleteConfigMap(oc *exutil.CLI, ns, name string) {
	_, err := oc.AsAdmin().WithoutNamespace().Run("delete").Args("configmap", name, "-n", ns).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
}

// CheckConfigMap waits for a ConfigMap to appear in the namespace.
func CheckConfigMap(oc *exutil.CLI, ns, name string) error {
	return wait.Poll(5*time.Second, 3*time.Minute, func() (bool, error) {
		output, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("cm", "-n", ns).Output()
		if err != nil {
			return false, nil
		}
		return strings.Contains(output, name), nil
	})
}

// CheckIPStackType reports whether service networks use IPv4, IPv6, or both.
func CheckIPStackType(oc *exutil.CLI) string {
	serviceNetwork, err := oc.WithoutNamespace().AsAdmin().Run("get").Args("network.operator", "cluster", "-o=jsonpath={.spec.serviceNetwork}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	ipv4 := strings.Count(serviceNetwork, ".") >= 2
	ipv6 := strings.Count(serviceNetwork, ":") >= 2
	switch {
	case ipv4 && ipv6:
		return "dualstack"
	case ipv6:
		return "ipv6single"
	case ipv4:
		return "ipv4single"
	default:
		return ""
	}
}

// GetNodeNameByPod returns the node currently running a pod.
func GetNodeNameByPod(oc *exutil.CLI, ns, podName string) string {
	output, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("pod", podName, "-n", ns, "-o=jsonpath={.spec.nodeName}").Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	return output
}

// ReadPodEnv returns matching environment variables from a pod.
func ReadPodEnv(oc *exutil.CLI, podName, ns, envName string) string {
	output, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", ns, podName, "--", "bash", "-c", "/usr/bin/env | grep "+envName).Output()
	if err != nil {
		return "NotFound"
	}
	return output
}

// ReadRouterPodEnv returns matching environment variables from a router pod.
func ReadRouterPodEnv(oc *exutil.CLI, podName, envName string) string {
	return ReadPodEnv(oc, podName, "openshift-ingress", envName)
}

// WaitForPodWithLabelAppear waits until at least one pod matches the label selector.
func WaitForPodWithLabelAppear(oc *exutil.CLI, ns, label string) error {
	return wait.Poll(5*time.Second, 3*time.Minute, func() (bool, error) {
		output, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("pod", "-n", ns, "-l", label).Output()
		return err == nil && len(output) > 0 && !strings.Contains(output, "No resources found"), nil
	})
}

// PollReadPodData retries a command in a pod until it succeeds or times out.
func PollReadPodData(oc *exutil.CLI, ns, podName, command, searchString string) string {
	var output string
	err := wait.Poll(5*time.Second, 60*time.Second, func() (bool, error) {
		var err error
		output, err = oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", ns, podName, "--", "bash", "-c", command+" | grep \""+searchString+"\"").Output()
		return err == nil, nil
	})
	compat_otp.AssertWaitPollNoErr(err, "timed out waiting for expected pod data")
	return output
}

// EnsureHaproxyBlockConfigMatchRegexp waits until HAProxy config contains each regular expression.
func EnsureHaproxyBlockConfigMatchRegexp(oc *exutil.CLI, routerPod, blockStart string, patterns []string) string {
	var config string
	err := wait.Poll(5*time.Second, 60*time.Second, func() (bool, error) {
		config = GetBlockConfig(oc, routerPod, blockStart)
		for _, pattern := range patterns {
			matched, err := regexp.MatchString(pattern, config)
			if err != nil {
				return false, err
			}
			if !matched {
				return false, nil
			}
		}
		return true, nil
	})
	compat_otp.AssertWaitPollNoErr(err, "timed out waiting for expected HAProxy configuration")
	return config
}

// IsCanaryRouteAvailable checks whether the ingress canary route responds successfully.
func IsCanaryRouteAvailable(oc *exutil.CLI) bool {
	host := GetByJsonPath(oc, "openshift-ingress-canary", "route/canary", "{.status.ingress[0].host}")
	// Compare the status code exactly: a substring match on the header block can
	// match a value such as "content-length: 1200". Poll instead of asserting so
	// that an unreachable canary route makes the caller skip the test.
	cmd := fmt.Sprintf("curl https://%s -skI -o /dev/null -w '%%{http_code}' --connect-timeout 10", host)
	waitErr := wait.Poll(1*time.Second, 60*time.Second, func() (bool, error) {
		output, err := exec.Command("bash", "-c", cmd).Output()
		if err != nil {
			return false, nil
		}
		return strings.TrimSpace(string(output)) == "200", nil
	})
	return waitErr == nil
}

// OpenSSLNewCA creates a self-signed certificate authority key and certificate.
func OpenSSLNewCA(caKey, caCert, subject string) {
	_, err := exec.Command("openssl", "req", "-x509", "-newkey", "rsa:4096", "-sha256", "-days", "365", "-keyout", caKey, "-out", caCert, "-nodes", "-subj", subject).Output()
	o.Expect(err).NotTo(o.HaveOccurred(), "failed to create a test certificate authority")
}

// OpenSSLNewCSR creates a private key and certificate signing request.
func OpenSSLNewCSR(serverKey, serverCSR, subject string) {
	_, err := exec.Command("openssl", "req", "-newkey", "rsa:4096", "-nodes", "-sha256", "-keyout", serverKey, "-out", serverCSR, "-subj", subject).Output()
	o.Expect(err).NotTo(o.HaveOccurred(), "failed to create a test certificate request")
}

// OpenSSLSignCSR signs a certificate signing request with the test CA.
func OpenSSLSignCSR(extensions, serverCSR, caCert, caKey, serverCert string) {
	extensionFile, err := ioutil.TempFile("", "router-test-extensions-*.conf")
	o.Expect(err).NotTo(o.HaveOccurred(), "failed to create temporary certificate extensions file")
	defer os.Remove(extensionFile.Name())
	_, err = extensionFile.WriteString(extensions)
	o.Expect(err).NotTo(o.HaveOccurred(), "failed to write temporary certificate extensions file")
	o.Expect(extensionFile.Close()).NotTo(o.HaveOccurred(), "failed to close temporary certificate extensions file")
	_, err = exec.Command("openssl", "x509", "-req", "-extfile", extensionFile.Name(), "-days", "30", "-in", serverCSR, "-CA", caCert, "-CAcreateserial", "-CAkey", caKey, "-out", serverCert).Output()
	o.Expect(err).NotTo(o.HaveOccurred(), "failed to sign a test certificate")
}
