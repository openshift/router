package router

import (
	"github.com/openshift/router-tests-extension/test/testdata"
	"fmt"
	"os"
	"path/filepath"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	compat_otp "github.com/openshift/origin/test/extended/util/compat_otp"
)

var _ = g.Describe("[sig-network-edge] Network_Edge Component_Router", func() {
	defer g.GinkgoRecover()

	var oc = compat_otp.NewCLI("router-tunning", compat_otp.KubeConfigPath())

	// incorporate OCP-40747, OCP-40748, OCP-40821 and OCP-40822
	// Test case creater: mjoseph@redhat.com - OCP-40747 The 'tune.maxrewrite' value can be modified with 'headerBufferMaxRewriteBytes' parameter
	// Test case creater: mjoseph@redhat.com - OCP-40748 The 'tune.bufsize' value can be modified with 'headerBufferBytes' parameter
	// Test case creater: mjoseph@redhat.com - OCP-40821 The 'tune.bufsize' and 'tune.maxwrite' values can be defined per haproxy router basis
	// Test case creater: shudili@redhat.com - OCP-40822 The 'headerBufferBytes' and 'headerBufferMaxRewriteBytes' strictly honours the default minimum values
	g.It("Author:mjoseph-Critical-40747-The 'tune.bufsize' and 'tune.maxwrite' values can be modified by ingresscontroller tuningOptions", func() {
		buildPruningBaseDir := testdata.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-tuning.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp40747a",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ingctrl2 = ingressControllerDescription{
				name:      "ocp40747b",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("1: Create a custom ingresscontroller, and get its router name")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)

		// OCP-40821 The 'tune.bufsize' and 'tune.maxwrite' values can be defined per haproxy router basis
		compat_otp.By("2: Check the haproxy config on the router pod for existing maxrewrite and bufsize value")
		ensureHaproxyBlockConfigContains(oc, routerpod, "global", []string{"tune.bufsize 16385", "tune.maxrewrite 4097"})

		compat_otp.By("3: Create a second custom ingresscontroller, and get its router name")
		baseDomain = getBaseDomain(oc)
		ingctrl2.domain = ingctrl2.name + "." + baseDomain
		defer ingctrl2.delete(oc)
		ingctrl2.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl2.name, "1")

		compat_otp.By("4: Patch the second ingresscontroller with maxrewrite and bufsize value")
		ingctrlResource2 := "ingresscontrollers/" + ingctrl2.name
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource2, "{\"spec\":{\"tuningOptions\" :{\"headerBufferBytes\": 18000, \"headerBufferMaxRewriteBytes\":10000}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl2.name, "2")
		newSecondRouterpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl2.name)

		compat_otp.By("5: Check the haproxy config on the router pod of second ingresscontroller for the tune.bufsize buffer value")
		ensureHaproxyBlockConfigContains(oc, newSecondRouterpod, "global", []string{"tune.bufsize 18000", "tune.maxrewrite 10000"})

		// OCP-40822 The 'headerBufferBytes' and 'headerBufferMaxRewriteBytes' strictly honours the default minimum values
		compat_otp.By("6: Patch ingresscontroller with minimum values and check whether it is configurable")
		output1, _ := oc.AsAdmin().WithoutNamespace().Run("patch").Args("ingresscontroller/ocp40747b", "-p", "{\"spec\":{\"tuningOptions\" :{\"headerBufferBytes\": 8192, \"headerBufferMaxRewriteBytes\":2048}}}", "--type=merge", "-n", ingctrl2.namespace).Output()
		o.Expect(output1).To(o.ContainSubstring(`The IngressController "ocp40747b" is invalid`))
		o.Expect(output1).To(o.ContainSubstring("spec.tuningOptions.headerBufferMaxRewriteBytes: Invalid value: 2048: spec.tuningOptions.headerBufferMaxRewriteBytes in body should be greater than or equal to 4096"))
		o.Expect(output1).To(o.ContainSubstring("spec.tuningOptions.headerBufferBytes: Invalid value: 8192: spec.tuningOptions.headerBufferBytes in body should be greater than or equal to 16384"))

		// OCP-40747 The 'tune.maxrewrite' value can be modified with 'headerBufferMaxRewriteBytes' parameter
		compat_otp.By("7: Patch ingresscontroller with tune.maxrewrite buffer value")
		ingctrlResource := "ingresscontrollers/" + ingctrl.name
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\":{\"tuningOptions\" :{\"headerBufferMaxRewriteBytes\": 8192}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		compat_otp.By("8: Check the haproxy config on the router pod for the tune.maxrewrite buffer value")
		newrouterpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		ensureHaproxyBlockConfigContains(oc, newrouterpod, "tune.maxrewrite", []string{"tune.maxrewrite 8192"})

		// OCP-40748 The 'tune.bufsize' value can be modified with 'headerBufferBytes' parameter
		compat_otp.By("9: Patch ingresscontroller with tune.bufsize buffer value")
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\":{\"tuningOptions\" :{\"headerBufferBytes\": 18000}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "3")

		compat_otp.By("10: check the haproxy config on the router pod for the tune.bufsize buffer value")
		newrouterpod = getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		ensureHaproxyBlockConfigContains(oc, newrouterpod, "tune.bufsize", []string{"tune.bufsize 18000"})
	})

	// incorporate OCP-41110 and OCP-41128
	// Test case creater: shudili@redhat.com - OCP-41110 The threadCount ingresscontroller parameter controls the nbthread option for the haproxy router
	// Test case creater: mjoseph@redhat.com - OCP-41128 Ingresscontroller should not accept invalid nbthread setting
	g.It("Author:shudili-LEVEL0-Critical-41110-The threadCount ingresscontroller parameter controls the nbthread option for the haproxy router", func() {
		buildPruningBaseDir := testdata.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp41110",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			threadcount        = "6"
			threadcountDefault = "4"
			threadcount1       = "-1"
			threadcount2       = "512"
			threadcount3       = `"abc"`
		)

		compat_otp.By("1: Create a ingresscontroller with threadCount set")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("2: Check the router env to verify the default value of ROUTER_THREADS is applied")
		podname := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		threadValue := readRouterPodEnv(oc, podname, "ROUTER_THREADS")
		o.Expect(threadValue).To(o.ContainSubstring("ROUTER_THREADS=" + threadcountDefault))

		compat_otp.By("3: Patch the new ingresscontroller with tuningOptions/threadCount " + threadcount)
		ingctrlResource := "ingresscontrollers/" + ingctrl.name
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\": {\"tuningOptions\": {\"threadCount\": "+threadcount+"}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		compat_otp.By("4: Check the router env to verify the PROXY variable ROUTER_THREADS with " + threadcount + " is applied")
		newpodname := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		dssearch := readRouterPodEnv(oc, newpodname, "ROUTER_THREADS")
		o.Expect(dssearch).To(o.ContainSubstring("ROUTER_THREADS=" + threadcount))

		compat_otp.By("5: Check the haproxy config on the router pod to ensure the nbthread is updated")
		ensureHaproxyBlockConfigContains(oc, newpodname, "nbthread", []string{"nbthread " + threadcount})

		// OCP-41128 Ingresscontroller should not accept invalid nbthread setting
		compat_otp.By("6: Patch the new ingresscontroller with negative(" + threadcount1 + ") value as threadCount")
		output1, _ := oc.AsAdmin().WithoutNamespace().Run("patch").Args(
			"ingresscontroller/"+ingctrl.name, "-p", "{\"spec\": {\"tuningOptions\": {\"threadCount\": "+threadcount1+"}}}",
			"--type=merge", "-n", ingctrl.namespace).Output()
		o.Expect(output1).To(o.ContainSubstring("Invalid value: -1: spec.tuningOptions.threadCount in body should be greater than or equal to 1"))

		compat_otp.By("7: Patch the new ingresscontroller with high(" + threadcount2 + ") value for threadCount")
		output2, _ := oc.AsAdmin().WithoutNamespace().Run("patch").Args(
			"ingresscontroller/"+ingctrl.name, "-p", "{\"spec\": {\"tuningOptions\": {\"threadCount\": "+threadcount2+"}}}",
			"--type=merge", "-n", ingctrl.namespace).Output()
		o.Expect(output2).To(o.ContainSubstring("Invalid value: 512: spec.tuningOptions.threadCount in body should be less than or equal to 64"))

		compat_otp.By("8: Patch the new ingresscontroller with string(" + threadcount3 + ") value for threadCount")
		output3, _ := oc.AsAdmin().WithoutNamespace().Run("patch").Args(
			"ingresscontroller/"+ingctrl.name, "-p", "{\"spec\": {\"tuningOptions\": {\"threadCount\": "+threadcount3+"}}}",
			"--type=merge", "-n", ingctrl.namespace).Output()
		o.Expect(output3).To(o.ContainSubstring(`Invalid value: "string": spec.tuningOptions.threadCount in body must be of type integer: "string"`))
	})

	// author: aiyengar@redhat.com
	g.It("Author:aiyengar-Critical-43105-The tcp client/server fin and default timeout for the ingresscontroller can be modified via tuningOptions parameterss", func() {
		buildPruningBaseDir := testdata.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "43105",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("Create a custom ingresscontroller, and get its router name")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)

		compat_otp.By("Verify the default server/client fin and default timeout values")
		ensureHaproxyBlockConfigContains(oc, routerpod, "defaults", []string{"timeout client 30s", "timeout client-fin 1s", "timeout server 30s", "timeout server-fin 1s"})

		compat_otp.By("Patch ingresscontroller with new timeout options")
		ingctrlResource := "ingresscontrollers/" + ingctrl.name
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\":{\"tuningOptions\" :{\"clientFinTimeout\": \"3s\",\"clientTimeout\":\"33s\",\"serverFinTimeout\":\"3s\",\"serverTimeout\":\"33s\"}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")
		newrouterpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)

		compat_otp.By("verify the timeout variables from the new router pods")
		checkenv := readRouterPodEnv(oc, newrouterpod, "TIMEOUT")
		o.Expect(checkenv).To(o.ContainSubstring(`ROUTER_CLIENT_FIN_TIMEOUT=3s`))
		o.Expect(checkenv).To(o.ContainSubstring(`ROUTER_DEFAULT_CLIENT_TIMEOUT=33s`))
		o.Expect(checkenv).To(o.ContainSubstring(`ROUTER_DEFAULT_SERVER_TIMEOUT=33`))
		o.Expect(checkenv).To(o.ContainSubstring(`ROUTER_DEFAULT_SERVER_FIN_TIMEOUT=3s`))
	})

	// incorporate OCP-43111 and OCP-43112
	// Test case creater: shudili@redhat.com - OCP-43111 The tcp client/server and tunnel timeouts for ingresscontroller will remain unchanged for negative values
	// Test case creater: shudili@redhat.com - OCP-43112 timeout tunnel parameter for the haproxy pods an be modified with TuningOptions option in the ingresscontroller
	g.It("Author:aiyengar-LEVEL0-Critical-43112-Timeout tunnel parameter for the haproxy pods an be modified with TuningOptions option in the ingresscontroller", func() {
		buildPruningBaseDir := testdata.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "43112",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("1: Create a custom ingresscontroller, and get its router name")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		compat_otp.By("Verify the default tls values")
		ensureHaproxyBlockConfigContains(oc, routerpod, "timeout tunnel", []string{"timeout tunnel 1h"})

		compat_otp.By("2: Patch ingresscontroller with a tunnel timeout option")
		ingctrlResource := "ingresscontrollers/" + ingctrl.name
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\":{\"tuningOptions\" :{\"tunnelTimeout\": \"2h\"}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")
		newrouterpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)

		compat_otp.By("3: Verify the new tls inspect timeout value in the router pod")
		checkenv := readRouterPodEnv(oc, newrouterpod, "ROUTER_DEFAULT_TUNNEL_TIMEOUT")
		o.Expect(checkenv).To(o.ContainSubstring(`ROUTER_DEFAULT_TUNNEL_TIMEOUT=2h`))

		// OCP-43111 The tcp client/server and tunnel timeouts for ingresscontroller will remain unchanged for negative values
		compat_otp.By("4: Patch ingresscontroller with negative values for the tuningOptions settings and check the ingress operator config post the change")
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, `{"spec":{"tuningOptions" :{"clientFinTimeout": "-7s","clientTimeout": "-33s","serverFinTimeout": "-3s","serverTimeout": "-27s","tlsInspectDelay": "-11s","tunnelTimeout": "-1h"}}}`)
		output := getByJsonPath(oc, "openshift-ingress-operator", "ingresscontroller/"+ingctrl.name, "{.spec.tuningOptions}")
		o.Expect(output).To(o.ContainSubstring("{\"clientFinTimeout\":\"-7s\",\"clientTimeout\":\"-33s\",\"reloadInterval\":\"0s\",\"serverFinTimeout\":\"-3s\",\"serverTimeout\":\"-27s\",\"tlsInspectDelay\":\"-11s\",\"tunnelTimeout\":\"-1h\"}"))

		compat_otp.By("5: Check the timeout option set in the haproxy pods post the changes applied")
		ensureHaproxyBlockConfigContains(oc, routerpod, "defaults", []string{"timeout connect 5s", "timeout client 30s", "timeout client-fin 1s", "timeout server 30s", "timeout server-fin 1s", "timeout tunnel 1h"})
	})

	// author: aiyengar@redhat.com
	g.It("Author:aiyengar-Critical-43113-Tcp inspect-delay for the haproxy pod can be modified via the TuningOptions parameters in the ingresscontroller", func() {
		buildPruningBaseDir := testdata.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "43113",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("Create a custom ingresscontroller, and get its router name")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)

		compat_otp.By("Verify the default tls values")
		ensureHaproxyBlockConfigContains(oc, routerpod, "tcp-request inspect-delay", []string{"tcp-request inspect-delay 5s"})

		compat_otp.By("Patch ingresscontroller with a tls inspect timeout option")
		ingctrlResource := "ingresscontrollers/" + ingctrl.name
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\":{\"tuningOptions\" :{\"tlsInspectDelay\": \"15s\"}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")
		newrouterpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)

		compat_otp.By("verify the new tls inspect timeout value in the router pod")
		checkenv := readRouterPodEnv(oc, newrouterpod, "ROUTER_INSPECT_DELAY")
		o.Expect(checkenv).To(o.ContainSubstring(`ROUTER_INSPECT_DELAY=15s`))
	})

	// incorporate OCP-50662 and OCP-50663
	// Test case creater: shudili@redhat.com - OCP-50662 Make ROUTER_BACKEND_CHECK_INTERVAL Configurable
	// Test case creater: shudili@redhat.com - OCP-50663 Negative Test of Make ROUTER_BACKEND_CHECK_INTERVAL Configurable
	g.It("Author:shudili-High-50662-Make ROUTER_BACKEND_CHECK_INTERVAL Configurable", func() {
		buildPruningBaseDir := testdata.FixturePath("testdata", "router")
		baseTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		extraParas := "    tuningOptions:\n      healthCheckInterval: 20s\n"
		customTemp1 := addExtraParametersToYamlFile(baseTemp, "spec:", extraParas)
		defer os.Remove(customTemp1)
		extraParas = "    tuningOptions:\n      healthCheckInterval: 100m\n"
		customTemp2 := addExtraParametersToYamlFile(baseTemp, "spec:", extraParas)
		defer os.Remove(customTemp2)
		ingctrl1 := ingressControllerDescription{
			name:      "ocp50662one",
			namespace: "openshift-ingress-operator",
			domain:    "",
			template:  customTemp1,
		}
		ingctrl2 := ingressControllerDescription{
			name:      "ocp50662two",
			namespace: "openshift-ingress-operator",
			domain:    "",
			template:  customTemp2,
		}
		ingctrlResource1 := "ingresscontrollers/" + ingctrl1.name
		ingctrlResource2 := "ingresscontrollers/" + ingctrl2.name

		compat_otp.By("1: Create two custom ICs for testing ROUTER_BACKEND_CHECK_INTERVAL")
		baseDomain := getBaseDomain(oc)
		ingctrl1.domain = ingctrl1.name + "." + baseDomain
		ingctrl2.domain = ingctrl2.name + "." + baseDomain
		defer ingctrl1.delete(oc)
		ingctrl1.create(oc)
		defer ingctrl2.delete(oc)
		ingctrl2.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl1.name, "1")
		ensureRouterDeployGenerationIs(oc, ingctrl2.name, "1")

		compat_otp.By("2: Check ROUTER_BACKEND_CHECK_INTERVAL env in a route pod of IC ocp50662one, which should be 20s")
		podname1 := getOneNewRouterPodFromRollingUpdate(oc, ingctrl1.name)
		hciSearch := readRouterPodEnv(oc, podname1, "ROUTER_BACKEND_CHECK_INTERVAL")
		o.Expect(hciSearch).To(o.ContainSubstring("ROUTER_BACKEND_CHECK_INTERVAL=20s"))

		compat_otp.By("3: Check ROUTER_BACKEND_CHECK_INTERVAL env in a route pod of IC ocp50662two, which should be 100m")
		podname2 := getOneNewRouterPodFromRollingUpdate(oc, ingctrl2.name)
		hciSearch = readRouterPodEnv(oc, podname2, "ROUTER_BACKEND_CHECK_INTERVAL")
		o.Expect(hciSearch).To(o.ContainSubstring("ROUTER_BACKEND_CHECK_INTERVAL=100m"))

		compat_otp.By("4: Patch tuningOptions/healthCheckInterval with max 2147483647ms to IC ocp50662one, while tuningOptions/healthCheckInterval 0s to the IC ocp50662two")
		healthCheckInterval := "2147483647ms"
		patchResourceAsAdmin(oc, ingctrl1.namespace, ingctrlResource1, "{\"spec\": {\"tuningOptions\": {\"healthCheckInterval\": \""+healthCheckInterval+"\"}}}")
		healthCheckInterval = "0s"
		patchResourceAsAdmin(oc, ingctrl2.namespace, ingctrlResource2, "{\"spec\": {\"tuningOptions\": {\"healthCheckInterval\": \""+healthCheckInterval+"\"}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl1.name, "2")
		ensureRouterDeployGenerationIs(oc, ingctrl2.name, "2")

		compat_otp.By("5: Check ROUTER_BACKEND_CHECK_INTERVAL env in a route pod of IC ocp50662one, which should be 2147483647ms")
		podname1 = getOneNewRouterPodFromRollingUpdate(oc, ingctrl1.name)
		hciSearch = readRouterPodEnv(oc, podname1, "ROUTER_BACKEND_CHECK_INTERVAL")
		o.Expect(hciSearch).To(o.ContainSubstring("ROUTER_BACKEND_CHECK_INTERVAL=2147483647ms"))

		compat_otp.By("6: Try to find the ROUTER_BACKEND_CHECK_INTERVAL env in a route pod which shouldn't be seen by default")
		podname2 = getOneNewRouterPodFromRollingUpdate(oc, ingctrl2.name)
		cmd := fmt.Sprintf("/usr/bin/env | grep %s", "ROUTER_BACKEND_CHECK_INTERVAL")
		_, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", podname2, "--", "bash", "-c", cmd).Output()
		o.Expect(err).To(o.HaveOccurred())

		// OCP-50663 Negative Test of Make ROUTER_BACKEND_CHECK_INTERVAL Configurable
		compat_otp.By("7: Try to patch tuningOptions/healthCheckInterval 2147483900ms which is larger than the max healthCheckInterval, to the ingress-controller")
		NegHealthCheckInterval := "2147483900ms"
		patchResourceAsAdmin(oc, ingctrl1.namespace, ingctrlResource1, "{\"spec\": {\"tuningOptions\": {\"healthCheckInterval\": \""+NegHealthCheckInterval+"\"}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl1.name, "2")

		compat_otp.By("8: Check ROUTER_BACKEND_CHECK_INTERVAL env in a route pod which should be the max: 2147483647ms")
		podname := getOneNewRouterPodFromRollingUpdate(oc, ingctrl1.name)
		hciSearch = readRouterPodEnv(oc, podname, "ROUTER_BACKEND_CHECK_INTERVAL")
		o.Expect(hciSearch).To(o.ContainSubstring("ROUTER_BACKEND_CHECK_INTERVAL=" + "2147483647ms"))

		compat_otp.By("9: Try to patch tuningOptions/healthCheckInterval -1s which is a minus value, to the ingress-controller")
		NegHealthCheckInterval = "-1s"
		output, err1 := oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource1, "-p", "{\"spec\": {\"tuningOptions\": {\"healthCheckInterval\": \""+NegHealthCheckInterval+"\"}}}", "--type=merge", "-n", ingctrl1.namespace).Output()
		o.Expect(err1).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Invalid value: \"-1s\""))

		compat_otp.By("10: Try to patch tuningOptions/healthCheckInterval abc which is a string, to the ingress-controller")
		NegHealthCheckInterval = "0abc"
		output, err2 := oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource1, "-p", "{\"spec\": {\"tuningOptions\": {\"healthCheckInterval\": \""+NegHealthCheckInterval+"\"}}}", "--type=merge", "-n", ingctrl1.namespace).Output()
		o.Expect(err2).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Invalid value: \"0abc\":"))
	})

	// incorporate OCP-50926 and OCP-50928
	// Test case creater: shudili@redhat.com - OCP-50926 Support a Configurable ROUTER_MAX_CONNECTIONS in HAproxy
	// Test case creater: shudili@redhat.com - OCP-50928 Negative test of Support a Configurable ROUTER_MAX_CONNECTIONS in HAproxy
	g.It("Author:shudili-High-50926-Support a Configurable ROUTER_MAX_CONNECTIONS in HAproxy", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("testdata", "router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-tuning.yaml")
			ingctrl             = ingressControllerDescription{
				name:      "ocp50926",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ingctrlResource = "ingresscontrollers/" + ingctrl.name
		)
		compat_otp.By("1: Create a custom IC with tuningOptions/maxConnections -1 specified by ingresscontroller-tuning.yaml")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("2: Check ROUTER_MAX_CONNECTIONS env under a route pod for the configured maxConnections -1,  which should be auto")
		podname := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		maxConnSearch := readRouterPodEnv(oc, podname, "ROUTER_MAX_CONNECTIONS")
		o.Expect(maxConnSearch).To(o.ContainSubstring("ROUTER_MAX_CONNECTIONS=auto"))

		compat_otp.By("3: Check maxconn in haproxy.config which won't appear after configured tuningOptions/maxConnections with -1")
		cmd := fmt.Sprintf("%s | grep \"%s\"", "cat haproxy.config", "maxconn")
		_, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", podname, "--", "bash", "-c", cmd).Output()
		o.Expect(err).To(o.HaveOccurred())

		compat_otp.By("4: Patch tuningOptions/maxConnections with 2000 to IC ocp50926")
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\": {\"tuningOptions\": {\"maxConnections\": 2000}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		compat_otp.By("5: Check ROUTER_MAX_CONNECTIONS env under a router pod of IC ocp50926, which should be 2000")
		podname = getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		maxConnSearch = readRouterPodEnv(oc, podname, "ROUTER_MAX_CONNECTIONS")
		o.Expect(maxConnSearch).To(o.ContainSubstring("ROUTER_MAX_CONNECTIONS=2000"))

		compat_otp.By("6: Check maxconn in haproxy.config under a router pod of IC ocp50926, which should be 2000")
		ensureHaproxyBlockConfigContains(oc, podname, "maxconn", []string{"maxconn 2000"})

		compat_otp.By("7: Patch tuningOptions/maxConnections with max 2000000 to IC ocp50926")
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\": {\"tuningOptions\": {\"maxConnections\": 2000000}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "3")

		compat_otp.By("8: Check ROUTER_MAX_CONNECTIONS env under a router pod of IC ocp50926,  which should be 2000000")
		podname = getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		maxConnSearch = readRouterPodEnv(oc, podname, "ROUTER_MAX_CONNECTIONS")
		o.Expect(maxConnSearch).To(o.ContainSubstring("ROUTER_MAX_CONNECTIONS=2000000"))

		compat_otp.By("9: Check maxconn in haproxy.config under a router pod of IC ocp50926, which should be 2000000")
		ensureHaproxyBlockConfigContains(oc, podname, "maxconn", []string{"maxconn 2000000"})

		// OCP-50928 Negative test of Support a Configurable ROUTER_MAX_CONNECTIONS in HAproxy
		compat_otp.By("10: Patch tuningOptions/maxConnections 0 to the IC")
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\": {\"tuningOptions\": {\"maxConnections\": 0}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "4")

		compat_otp.By("11: Try to Check ROUTER_MAX_CONNECTIONS env in a route pod set to the default maxConnections by 0")
		podname = getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		cmd = fmt.Sprintf("/usr/bin/env | grep %s", "ROUTER_MAX_CONNECTIONS")
		_, err = oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", podname, "--", "bash", "-c", cmd).Output()
		o.Expect(err).To(o.HaveOccurred())

		compat_otp.By("12: Check maxconn in haproxy.config under a router pod of the IC , which should be 50000")
		ensureHaproxyBlockConfigContains(oc, podname, "maxconn", []string{"maxconn 50000"})

		compat_otp.By("13: Try to patch the ingress-controller with tuningOptions/maxConnections 1999, which is less than the min 2000")
		NegMaxConnections := "1999"
		output, err2 := oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", "{\"spec\": {\"tuningOptions\": {\"maxConnections\": "+NegMaxConnections+"}}}", "--type=merge", "-n", ingctrl.namespace).Output()
		o.Expect(err2).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Unsupported value: " + NegMaxConnections + ": supported values: \"-1\", \"0\""))

		compat_otp.By("14: Try to patch the ingress-controller with tuningOptions/maxConnections 2000001, which is a larger than the max 2000000")
		NegMaxConnections = "2000001"
		output, err2 = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", "{\"spec\": {\"tuningOptions\": {\"maxConnections\": "+NegMaxConnections+"}}}", "--type=merge", "-n", ingctrl.namespace).Output()
		o.Expect(err2).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Unsupported value: " + NegMaxConnections + ": supported values: \"-1\", \"0\""))

		compat_otp.By("15: Try to patch the ingress-controller with tuningOptions/maxConnections abc, which is a string")
		NegMaxConnections = "abc"
		output, err2 = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", "{\"spec\": {\"tuningOptions\": {\"maxConnections\": \""+NegMaxConnections+"\"}}}", "--type=merge", "-n", ingctrl.namespace).Output()
		o.Expect(err2).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Invalid value: \"string\": spec.tuningOptions.maxConnections in body must be of type integer"))
	})

	// incorporate OCP-53605 and OCP-53608
	// Test case creater: shudili@redhat.com - OCP-53605 Expose a Configurable Reload Interval in HAproxy
	// Test case creater: shudili@redhat.com - OCP-53608 Negative Test of Expose a Configurable Reload Interval in HAproxy
	g.It("Author:shudili-High-53605-Expose a Configurable Reload Interval in HAproxy", func() {
		buildPruningBaseDir := testdata.FixturePath("testdata", "router")
		baseTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		extraParas := "    tuningOptions:\n      reloadInterval: 15s\n"
		customTemp1 := addExtraParametersToYamlFile(baseTemp, "spec:", extraParas)
		defer os.Remove(customTemp1)
		extraParas = "    tuningOptions:\n      reloadInterval: 120s\n"
		customTemp2 := addExtraParametersToYamlFile(baseTemp, "spec:", extraParas)
		defer os.Remove(customTemp2)
		ingctrl1 := ingressControllerDescription{
			name:      "ocp53605one",
			namespace: "openshift-ingress-operator",
			domain:    "",
			template:  customTemp1,
		}
		ingctrl2 := ingressControllerDescription{
			name:      "ocp53605two",
			namespace: "openshift-ingress-operator",
			domain:    "",
			template:  customTemp2,
		}
		ingctrlResource1 := "ingresscontrollers/" + ingctrl1.name
		ingctrlResource2 := "ingresscontrollers/" + ingctrl2.name

		compat_otp.By("1: Create two custom ICs for testing router reload interval")
		baseDomain := getBaseDomain(oc)
		ingctrl1.domain = ingctrl1.name + "." + baseDomain
		ingctrl2.domain = ingctrl2.name + "." + baseDomain
		defer ingctrl1.delete(oc)
		ingctrl1.create(oc)
		defer ingctrl2.delete(oc)
		ingctrl2.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl1.name, "1")
		ensureRouterDeployGenerationIs(oc, ingctrl2.name, "1")

		compat_otp.By("2: Check RELOAD_INTERVAL env in a route pod of IC ocp53605one, which should be 15s")
		podname1 := getOneNewRouterPodFromRollingUpdate(oc, ingctrl1.name)
		riSearch := readRouterPodEnv(oc, podname1, "RELOAD_INTERVAL")
		o.Expect(riSearch).To(o.ContainSubstring("RELOAD_INTERVAL=15s"))

		compat_otp.By("3: Check RELOAD_INTERVAL env in a route pod of IC ocp53605two, which should be 2m")
		podname2 := getOneNewRouterPodFromRollingUpdate(oc, ingctrl2.name)
		riSearch = readRouterPodEnv(oc, podname2, "RELOAD_INTERVAL")
		o.Expect(riSearch).To(o.ContainSubstring("RELOAD_INTERVAL=2m"))

		compat_otp.By("4: Patch tuningOptions/reloadInterval with other valid unit m, for exmpale 1m to IC ocp53605one, while patch it with 0s to IC ocp53605two")
		patchResourceAsAdmin(oc, ingctrl1.namespace, ingctrlResource1, "{\"spec\": {\"tuningOptions\": {\"reloadInterval\": \"1m\"}}}")
		patchResourceAsAdmin(oc, ingctrl2.namespace, ingctrlResource2, "{\"spec\": {\"tuningOptions\": {\"reloadInterval\": \"0s\"}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl1.name, "2")
		ensureRouterDeployGenerationIs(oc, ingctrl2.name, "2")

		compat_otp.By("5: Check RELOAD_INTERVAL env in a route pod of IC ocp53605one which should be 1m")
		podname1 = getOneNewRouterPodFromRollingUpdate(oc, ingctrl1.name)
		riSearch = readRouterPodEnv(oc, podname1, "RELOAD_INTERVAL")
		o.Expect(riSearch).To(o.ContainSubstring("RELOAD_INTERVAL=1m"))

		compat_otp.By("6: Check RELOAD_INTERVAL env in a route pod of IC ocp53605two, which is the default 5s")
		podname2 = getOneNewRouterPodFromRollingUpdate(oc, ingctrl2.name)
		riSearch = readRouterPodEnv(oc, podname2, "RELOAD_INTERVAL")
		o.Expect(riSearch).To(o.ContainSubstring("RELOAD_INTERVAL=5s"))

		// OCP-53608 Negative Test of Expose a Configurable Reload Interval in HAproxy
		compat_otp.By("7: Try to patch tuningOptions/reloadInterval 121s which is larger than the max 120s, to the ingress-controller")
		NegReloadInterval := "121s"
		patchResourceAsAdmin(oc, ingctrl1.namespace, ingctrlResource1, "{\"spec\": {\"tuningOptions\": {\"reloadInterval\": \""+NegReloadInterval+"\"}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl1.name, "3")

		compat_otp.By("8: Check RELOAD_INTERVAL env in a route pod which should be the max 2m")
		podname := getOneNewRouterPodFromRollingUpdate(oc, ingctrl1.name)
		riSearch = readRouterPodEnv(oc, podname, "RELOAD_INTERVAL")
		o.Expect(riSearch).To(o.ContainSubstring("RELOAD_INTERVAL=2m"))

		compat_otp.By("9: Try to patch tuningOptions/reloadInterval 0.5s which is less than the min 1s, to the ingress-controller")
		NegReloadInterval = "0.5s"
		patchResourceAsAdmin(oc, ingctrl1.namespace, ingctrlResource1, "{\"spec\": {\"tuningOptions\": {\"reloadInterval\": \""+NegReloadInterval+"\"}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl1.name, "4")

		compat_otp.By("10: Check RELOAD_INTERVAL env in a route pod which should be the min 1s")
		podname = getOneNewRouterPodFromRollingUpdate(oc, ingctrl1.name)
		riSearch = readRouterPodEnv(oc, podname, "RELOAD_INTERVAL")
		o.Expect(riSearch).To(o.ContainSubstring("RELOAD_INTERVAL=1s"))

		compat_otp.By("11: Try to patch tuningOptions/reloadInterval -1s which is a minus value, to the ingress-controller")
		NegReloadInterval = "-1s"
		output, errCfg := patchResourceAsAdminAndGetLog(oc, ingctrl1.namespace, ingctrlResource1, "{\"spec\": {\"tuningOptions\": {\"reloadInterval\": \""+NegReloadInterval+"\"}}}")
		o.Expect(errCfg).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Invalid value: \"" + NegReloadInterval + "\""))

		compat_otp.By("12: Try to patch tuningOptions/reloadInterval 1abc which is a string, to the ingress-controller")
		NegReloadInterval = "1abc"
		output, errCfg = patchResourceAsAdminAndGetLog(oc, ingctrl1.namespace, ingctrlResource1, "{\"spec\": {\"tuningOptions\": {\"reloadInterval\": \""+NegReloadInterval+"\"}}}")
		o.Expect(errCfg).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Invalid value: \"" + NegReloadInterval + "\""))

		compat_otp.By("13: Try to patch tuningOptions/reloadInterval 012 s which contains a space character, to the ingress-controller")
		NegReloadInterval = "012 s"
		output, errCfg = patchResourceAsAdminAndGetLog(oc, ingctrl1.namespace, ingctrlResource1, "{\"spec\": {\"tuningOptions\": {\"reloadInterval\": \""+NegReloadInterval+"\"}}}")
		o.Expect(errCfg).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Invalid value: \"" + NegReloadInterval + "\""))
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-LEVEL0-High-55367-Default HAProxy maxconn value to 50000 for OCP 4.12 and later", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("testdata", "router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			ingctrl             = ingressControllerDescription{
				name:      "ocp55367",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("Create an custom ingresscontroller for testing ROUTER_MAX_CONNECTIONS")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		ingctrlResource := "ingresscontrollers/" + ingctrl.name
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("Check default value of ROUTER_MAX_CONNECTIONS env in a route pod, which shouldn't appear in it")
		podname := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		cmd := fmt.Sprintf("/usr/bin/env | grep %s", "ROUTER_MAX_CONNECTIONS")
		_, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", podname, "--", "bash", "-c", cmd).Output()
		o.Expect(err).To(o.HaveOccurred())

		compat_otp.By("Check maxconn in haproxy.config which should be 50000")
		ensureHaproxyBlockConfigContains(oc, podname, "maxconn", []string{"maxconn 50000"})

		compat_otp.By("Patch tuningOptions/maxConnections with null to the ingress-controller")
		maxConnections := "null"
		jpath := "{.status.observedGeneration}"
		observedGen1 := getByJsonPath(oc, "openshift-ingress", "deployment.apps/router-default", jpath)
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\": {\"tuningOptions\": {\"maxConnections\": "+maxConnections+"}}}")
		observedGen2 := getByJsonPath(oc, "openshift-ingress", "deployment.apps/router-default", jpath)
		o.Expect(observedGen1).To(o.ContainSubstring(observedGen2))

		compat_otp.By("Check ROUTER_MAX_CONNECTIONS env in a route pod which shouldn't appear in it by default")
		podname = getOneRouterPodNameByIC(oc, ingctrl.name)
		_, err = oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", podname, "--", "bash", "-c", cmd).Output()
		o.Expect(err).To(o.HaveOccurred())

		compat_otp.By("Check maxconn in haproxy.config which should be 50000")
		ensureHaproxyBlockConfigContains(oc, podname, "maxconn", []string{"maxconn 50000"})

		compat_otp.By("Patch tuningOptions/maxConnections 50000 to the ingress-controller")
		maxConnections = "500000"
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\": {\"tuningOptions\": {\"maxConnections\": "+maxConnections+"}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		compat_otp.By("Check ROUTER_MAX_CONNECTIONS env in a route pod which should be " + maxConnections)
		podname = getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		maxConnSearch := readRouterPodEnv(oc, podname, "ROUTER_MAX_CONNECTIONS")
		o.Expect(maxConnSearch).To(o.ContainSubstring("ROUTER_MAX_CONNECTIONS=" + maxConnections))

		compat_otp.By("Check maxconn in haproxy.config which should be 50000")
		ensureHaproxyBlockConfigContains(oc, podname, "maxconn", []string{"maxconn 50000"})
	})

	// OCPBUGS-61858
	// author: shudili@redhat.com
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-High-86153-Supporting HTTPKeepAliveTimeout tuning option", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("testdata", "router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			ingctrl             = ingressControllerDescription{
				name:      "ocp86153",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ingctrlResource = "ingresscontrollers/" + ingctrl.name
		)

		compat_otp.By("1.0: Create an custom ingresscontroller for the testing")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("2.0: Check the default http-keep-alive in haproxy.config which should be 300s")
		podname := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		ensureHaproxyBlockConfigContains(oc, podname, "defaults", []string{"http-keep-alive 300s"})

		compat_otp.By("2.1: Check default value of ROUTER_SLOWLORIS_HTTP_KEEPALIVE env in a route pod, which shouldn't appear")
		cmd := fmt.Sprintf("/usr/bin/env | grep %s", "ROUTER_SLOWLORIS_HTTP_KEEPALIVE")
		_, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", podname, "--", "bash", "-c", cmd).Output()
		o.Expect(err).To(o.HaveOccurred())

		compat_otp.By("3.0: Patch tuningOptions/httpKeepAliveTimeout with 50s to the ingress-controller")
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, `{"spec": {"tuningOptions": {"httpKeepAliveTimeout": "50s"}}}`)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		compat_otp.By("3.1: Check the tuningOptions.httpKeepAliveTimeout in the ingress-controller which should be 50s")
		tuningOptionsKeepAlive := getByJsonPath(oc, ingctrl.namespace, ingctrlResource, "{.spec.tuningOptions.httpKeepAliveTimeout}")
		o.Expect(tuningOptionsKeepAlive).To(o.ContainSubstring("50s"))

		compat_otp.By("3.2: Check http-keep-alive in haproxy.config which should be 50s")
		podname = getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		ensureHaproxyBlockConfigContains(oc, podname, "defaults", []string{"http-keep-alive 50s"})

		compat_otp.By("3.3: Check ROUTER_SLOWLORIS_HTTP_KEEPALIVE env in a route pod which should be 50s")
		podname = getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		keepAlive := readRouterPodEnv(oc, podname, "ROUTER_SLOWLORIS_HTTP_KEEPALIVE")
		o.Expect(keepAlive).To(o.ContainSubstring("ROUTER_SLOWLORIS_HTTP_KEEPALIVE=50s"))

		compat_otp.By("4.0: Try to patch tuningOptions/httpKeepAliveTimeout with 50m, which is larger than the max 15m to the ingress-controller")
		output := patchResourceAsAdminWithErrorOutput(oc, ingctrl.namespace, ingctrlResource, `{"spec": {"tuningOptions": {"httpKeepAliveTimeout": "50m"}}}`)
		o.Expect(output).To(o.ContainSubstring("httpKeepAliveTimeout must be less than or equal to 15 minutes"))

		compat_otp.By("5.0: Try to patch tuningOptions/httpKeepAliveTimeout with 1h, the unit of which is not supported")
		output = patchResourceAsAdminWithErrorOutput(oc, ingctrl.namespace, ingctrlResource, `{"spec": {"tuningOptions": {"httpKeepAliveTimeout": "1h"}}}`)
		o.Expect(output).To(o.ContainSubstring("httpKeepAliveTimeout must be a valid duration string composed of an unsigned integer value, optionally followed by a decimal fraction and a unit suffix (ms, s, m)"))
	})
})
