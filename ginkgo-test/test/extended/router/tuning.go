package router

import (
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

	// author: mjoseph@redhat.com
	g.It("Author:mjoseph-Critical-40747-The 'tune.maxrewrite' value can be modified with 'headerBufferMaxRewriteBytes' parameter", func() {
		buildPruningBaseDir := compat_otp.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-tuning.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp40747",
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

		compat_otp.By("Patch ingresscontroller with tune.maxrewrite buffer value")
		ingctrlResource := "ingresscontrollers/" + ingctrl.name
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\":{\"tuningOptions\" :{\"headerBufferMaxRewriteBytes\": 8192}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		compat_otp.By("check the haproxy config on the router pod for the tune.maxrewrite buffer value")
		newrouterpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		output2, _ := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", newrouterpod, "--", "bash", "-c", "cat haproxy.config | grep tune.maxrewrite").Output()
		o.Expect(output2).To(o.ContainSubstring(`tune.maxrewrite 8192`))
	})

	// author: mjoseph@redhat.com
	g.It("Author:mjoseph-Critical-40748-The 'tune.bufsize' value can be modified with 'headerBufferBytes' parameter", func() {
		buildPruningBaseDir := compat_otp.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-tuning.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp40748",
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

		compat_otp.By("Patch ingresscontroller with tune.bufsize buffer value")
		ingctrlResource := "ingresscontrollers/" + ingctrl.name
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\":{\"tuningOptions\" :{\"headerBufferBytes\": 18000}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		compat_otp.By("check the haproxy config on the router pod for the tune.bufsize buffer value")
		newrouterpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		output2, _ := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", newrouterpod, "--", "bash", "-c", "cat haproxy.config | grep tune.bufsize").Output()
		o.Expect(output2).To(o.ContainSubstring(`tune.bufsize 18000`))
	})

	// author: mjoseph@redhat.com
	g.It("Author:mjoseph-High-40821-The 'tune.bufsize' and 'tune.maxwrite' values can be defined per haproxy router basis", func() {
		buildPruningBaseDir := compat_otp.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-tuning.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp40821a",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ingctrl2 = ingressControllerDescription{
				name:      "ocp40821b",
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

		compat_otp.By("check the haproxy config on the router pod for existing maxrewrite and bufsize value")
		output, _ := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", routerpod, "--", "bash", "-c", "cat haproxy.config | grep -e tune.maxrewrite -e tune.bufsize").Output()
		o.Expect(output).To(o.ContainSubstring(`tune.maxrewrite 4097`))
		o.Expect(output).To(o.ContainSubstring(`tune.bufsize 16385`))

		compat_otp.By("Create a second custom ingresscontroller, and get its router name")
		baseDomain = getBaseDomain(oc)
		ingctrl2.domain = ingctrl2.name + "." + baseDomain
		defer ingctrl2.delete(oc)
		ingctrl2.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl2.name, "1")

		compat_otp.By("Patch the second ingresscontroller with maxrewrite and bufsize value")
		ingctrlResource := "ingresscontrollers/" + ingctrl2.name
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\":{\"tuningOptions\" :{\"headerBufferBytes\": 18000, \"headerBufferMaxRewriteBytes\":10000}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl2.name, "2")
		newSecondRouterpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl2.name)

		compat_otp.By("check the haproxy config on the router pod of second ingresscontroller for the tune.bufsize buffer value")
		output1, _ := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", newSecondRouterpod, "--", "bash", "-c", "cat haproxy.config | grep -e tune.maxrewrite -e tune.bufsize").Output()
		o.Expect(output1).To(o.ContainSubstring(`tune.maxrewrite 10000`))
		o.Expect(output1).To(o.ContainSubstring(`tune.bufsize 18000`))
	})

	// author: mjoseph@redhat.com
	g.It("Author:mjoseph-Low-40822-The 'headerBufferBytes' and 'headerBufferMaxRewriteBytes' strictly honours the default minimum values", func() {
		buildPruningBaseDir := compat_otp.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-tuning.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp40822",
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

		compat_otp.By("check the existing maxrewrite and bufsize value")
		output, _ := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", routerpod, "--", "bash", "-c", "/usr/bin/env | grep -ie buf -ie rewrite").Output()
		o.Expect(output).To(o.ContainSubstring(`ROUTER_BUF_SIZE=16385`))
		o.Expect(output).To(o.ContainSubstring(`ROUTER_MAX_REWRITE_SIZE=4097`))

		compat_otp.By("Patch ingresscontroller with minimum values and check whether it is configurable")
		output1, _ := oc.AsAdmin().WithoutNamespace().Run("patch").Args("ingresscontroller/ocp40822", "-p", "{\"spec\":{\"tuningOptions\" :{\"headerBufferBytes\": 8192, \"headerBufferMaxRewriteBytes\":2048}}}", "--type=merge", "-n", ingctrl.namespace).Output()
		o.Expect(output1).To(o.ContainSubstring(`The IngressController "ocp40822" is invalid`))
		o.Expect(output1).To(o.ContainSubstring("spec.tuningOptions.headerBufferMaxRewriteBytes: Invalid value: 2048: spec.tuningOptions.headerBufferMaxRewriteBytes in body should be greater than or equal to 4096"))
		o.Expect(output1).To(o.ContainSubstring("spec.tuningOptions.headerBufferBytes: Invalid value: 8192: spec.tuningOptions.headerBufferBytes in body should be greater than or equal to 16384"))
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-LEVEL0-Critical-41110-The threadCount ingresscontroller parameter controls the nbthread option for the haproxy router", func() {
		buildPruningBaseDir := compat_otp.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp41110",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			threadcount = "6"
		)

		compat_otp.By("Create a ingresscontroller with threadCount set")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("Patch the new ingresscontroller with tuningOptions/threadCount " + threadcount)
		ingctrlResource := "ingresscontrollers/" + ingctrl.name
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\": {\"tuningOptions\": {\"threadCount\": "+threadcount+"}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		compat_otp.By("Check the router env to verify the PROXY variable ROUTER_THREADS with " + threadcount + " is applied")
		newpodname := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		dssearch := readRouterPodEnv(oc, newpodname, "ROUTER_THREADS")
		o.Expect(dssearch).To(o.ContainSubstring("ROUTER_THREADS=" + threadcount))
		compat_otp.By("check the haproxy config on the router pod to ensure the nbthread is updated")

		nbthread := readRouterPodData(oc, newpodname, "cat haproxy.config", "nbthread")
		o.Expect(nbthread).To(o.ContainSubstring("nbthread " + threadcount))
	})

	// author: mjoseph@redhat.com
	g.It("Author:mjoseph-Low-41128-Ingresscontroller should not accept invalid nbthread setting", func() {
		buildPruningBaseDir := compat_otp.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp41128",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			threadcountDefault = "4"
			threadcount1       = "-1"
			threadcount2       = "512"
			threadcount3       = `"abc"`
		)

		compat_otp.By("create a custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("Patch the new ingresscontroller with negative(" + threadcount1 + ") value as threadCount")
		output1, _ := oc.AsAdmin().WithoutNamespace().Run("patch").Args(
			"ingresscontroller/"+ingctrl.name, "-p", "{\"spec\": {\"tuningOptions\": {\"threadCount\": "+threadcount1+"}}}",
			"--type=merge", "-n", ingctrl.namespace).Output()
		o.Expect(output1).To(o.ContainSubstring("Invalid value: -1: spec.tuningOptions.threadCount in body should be greater than or equal to 1"))

		compat_otp.By("Patch the new ingresscontroller with high(" + threadcount2 + ") value for threadCount")
		output2, _ := oc.AsAdmin().WithoutNamespace().Run("patch").Args(
			"ingresscontroller/"+ingctrl.name, "-p", "{\"spec\": {\"tuningOptions\": {\"threadCount\": "+threadcount2+"}}}",
			"--type=merge", "-n", ingctrl.namespace).Output()
		o.Expect(output2).To(o.ContainSubstring("Invalid value: 512: spec.tuningOptions.threadCount in body should be less than or equal to 64"))

		compat_otp.By("Patch the new ingresscontroller with string(" + threadcount3 + ") value for threadCount")
		output3, _ := oc.AsAdmin().WithoutNamespace().Run("patch").Args(
			"ingresscontroller/"+ingctrl.name, "-p", "{\"spec\": {\"tuningOptions\": {\"threadCount\": "+threadcount3+"}}}",
			"--type=merge", "-n", ingctrl.namespace).Output()
		o.Expect(output3).To(o.ContainSubstring(`Invalid value: "string": spec.tuningOptions.threadCount in body must be of type integer: "string"`))

		compat_otp.By("Check the router env to verify the default value of ROUTER_THREADS is applied")
		podname := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		threadValue := readRouterPodEnv(oc, podname, "ROUTER_THREADS")
		o.Expect(threadValue).To(o.ContainSubstring("ROUTER_THREADS=" + threadcountDefault))
	})

	// author: aiyengar@redhat.com
	g.It("Author:aiyengar-Critical-43105-The tcp client/server fin and default timeout for the ingresscontroller can be modified via tuningOptions parameterss", func() {
		buildPruningBaseDir := compat_otp.FixturePath("testdata", "router")
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
		checkoutput, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", routerpod, "--", "bash", "-c", `cat haproxy.config | grep -we "timeout client" -we "timeout client-fin" -we "timeout server" -we "timeout server-fin"`).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(checkoutput).To(o.ContainSubstring(`timeout client 30s`))
		o.Expect(checkoutput).To(o.ContainSubstring(`timeout client-fin 1s`))
		o.Expect(checkoutput).To(o.ContainSubstring(`timeout server 30s`))
		o.Expect(checkoutput).To(o.ContainSubstring(`timeout server-fin 1s`))

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

	// author: aiyengar@redhat.com
	g.It("Author:aiyengar-Medium-43111-The tcp client/server and tunnel timeouts for ingresscontroller will remain unchanged for negative values", func() {
		buildPruningBaseDir := compat_otp.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "43111",
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

		compat_otp.By("Patch ingresscontroller with negative values for the tuningOptions settings and check the ingress operator config post the change")
		ingctrlResource := "ingresscontrollers/" + ingctrl.name
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, `{"spec":{"tuningOptions" :{"clientFinTimeout": "-7s","clientTimeout": "-33s","serverFinTimeout": "-3s","serverTimeout": "-27s","tlsInspectDelay": "-11s","tunnelTimeout": "-1h"}}}`)
		output := getByJsonPath(oc, "openshift-ingress-operator", "ingresscontroller/"+ingctrl.name, "{.spec.tuningOptions}")
		o.Expect(output).To(o.ContainSubstring("{\"clientFinTimeout\":\"-7s\",\"clientTimeout\":\"-33s\",\"reloadInterval\":\"0s\",\"serverFinTimeout\":\"-3s\",\"serverTimeout\":\"-27s\",\"tlsInspectDelay\":\"-11s\",\"tunnelTimeout\":\"-1h\"}"))

		compat_otp.By("Check the timeout option set in the haproxy pods post the changes applied")
		checktimeout := readRouterPodData(oc, routerpod, "cat haproxy.config", "timeout")
		o.Expect(checktimeout).To(o.ContainSubstring("timeout connect 5s"))
		o.Expect(checktimeout).To(o.ContainSubstring("timeout client 30s"))
		o.Expect(checktimeout).To(o.ContainSubstring("timeout client-fin 1s"))
		o.Expect(checktimeout).To(o.ContainSubstring("timeout server 30s"))
		o.Expect(checktimeout).To(o.ContainSubstring("timeout server-fin 1s"))
		o.Expect(checktimeout).To(o.ContainSubstring("timeout tunnel 1h"))
	})

	// author: aiyengar@redhat.com
	g.It("Author:aiyengar-LEVEL0-Critical-43112-timeout tunnel parameter for the haproxy pods an be modified with TuningOptions option in the ingresscontroller", func() {
		buildPruningBaseDir := compat_otp.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "43112",
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
		checkoutput, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", routerpod, "--", "bash", "-c", `cat haproxy.config | grep -w "timeout tunnel"`).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(checkoutput).To(o.ContainSubstring(`timeout tunnel 1h`))

		compat_otp.By("Patch ingresscontroller with a tunnel timeout option")
		ingctrlResource := "ingresscontrollers/" + ingctrl.name
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\":{\"tuningOptions\" :{\"tunnelTimeout\": \"2h\"}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")
		newrouterpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)

		compat_otp.By("verify the new tls inspect timeout value in the router pod")
		checkenv := readRouterPodEnv(oc, newrouterpod, "ROUTER_DEFAULT_TUNNEL_TIMEOUT")
		o.Expect(checkenv).To(o.ContainSubstring(`ROUTER_DEFAULT_TUNNEL_TIMEOUT=2h`))
	})

	// author: aiyengar@redhat.com
	g.It("Author:aiyengar-Critical-43113-Tcp inspect-delay for the haproxy pod can be modified via the TuningOptions parameters in the ingresscontroller", func() {
		buildPruningBaseDir := compat_otp.FixturePath("testdata", "router")
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
		checkoutput, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", routerpod, "--", "bash", "-c", `cat haproxy.config | grep -w "inspect-delay"| uniq`).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(checkoutput).To(o.ContainSubstring(`tcp-request inspect-delay 5s`))

		compat_otp.By("Patch ingresscontroller with a tls inspect timeout option")
		ingctrlResource := "ingresscontrollers/" + ingctrl.name
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\":{\"tuningOptions\" :{\"tlsInspectDelay\": \"15s\"}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")
		newrouterpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)

		compat_otp.By("verify the new tls inspect timeout value in the router pod")
		checkenv := readRouterPodEnv(oc, newrouterpod, "ROUTER_INSPECT_DELAY")
		o.Expect(checkenv).To(o.ContainSubstring(`ROUTER_INSPECT_DELAY=15s`))
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-High-50662-Make ROUTER_BACKEND_CHECK_INTERVAL Configurable", func() {
		buildPruningBaseDir := compat_otp.FixturePath("testdata", "router")
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

		compat_otp.By("Create two custom ICs for testing ROUTER_BACKEND_CHECK_INTERVAL")
		baseDomain := getBaseDomain(oc)
		ingctrl1.domain = ingctrl1.name + "." + baseDomain
		ingctrl2.domain = ingctrl2.name + "." + baseDomain
		defer ingctrl1.delete(oc)
		ingctrl1.create(oc)
		defer ingctrl2.delete(oc)
		ingctrl2.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl1.name, "1")
		ensureRouterDeployGenerationIs(oc, ingctrl2.name, "1")

		compat_otp.By("Check ROUTER_BACKEND_CHECK_INTERVAL env in a route pod of IC ocp50662one, which should be 20s")
		podname1 := getOneNewRouterPodFromRollingUpdate(oc, ingctrl1.name)
		hciSearch := readRouterPodEnv(oc, podname1, "ROUTER_BACKEND_CHECK_INTERVAL")
		o.Expect(hciSearch).To(o.ContainSubstring("ROUTER_BACKEND_CHECK_INTERVAL=20s"))

		compat_otp.By("Check ROUTER_BACKEND_CHECK_INTERVAL env in a route pod of IC ocp50662two, which should be 100m")
		podname2 := getOneNewRouterPodFromRollingUpdate(oc, ingctrl2.name)
		hciSearch = readRouterPodEnv(oc, podname2, "ROUTER_BACKEND_CHECK_INTERVAL")
		o.Expect(hciSearch).To(o.ContainSubstring("ROUTER_BACKEND_CHECK_INTERVAL=100m"))

		compat_otp.By("Patch tuningOptions/healthCheckInterval with max 2147483647ms to IC ocp50662one, while tuningOptions/healthCheckInterval 0s to the IC ocp50662two")
		healthCheckInterval := "2147483647ms"
		patchResourceAsAdmin(oc, ingctrl1.namespace, ingctrlResource1, "{\"spec\": {\"tuningOptions\": {\"healthCheckInterval\": \""+healthCheckInterval+"\"}}}")
		healthCheckInterval = "0s"
		patchResourceAsAdmin(oc, ingctrl2.namespace, ingctrlResource2, "{\"spec\": {\"tuningOptions\": {\"healthCheckInterval\": \""+healthCheckInterval+"\"}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl1.name, "2")
		ensureRouterDeployGenerationIs(oc, ingctrl2.name, "2")

		compat_otp.By("Check ROUTER_BACKEND_CHECK_INTERVAL env in a route pod of IC ocp50662one, which should be 2147483647ms")
		podname1 = getOneNewRouterPodFromRollingUpdate(oc, ingctrl1.name)
		hciSearch = readRouterPodEnv(oc, podname1, "ROUTER_BACKEND_CHECK_INTERVAL")
		o.Expect(hciSearch).To(o.ContainSubstring("ROUTER_BACKEND_CHECK_INTERVAL=2147483647ms"))

		compat_otp.By("Try to find the ROUTER_BACKEND_CHECK_INTERVAL env in a route pod which shouldn't be seen by default")
		podname2 = getOneNewRouterPodFromRollingUpdate(oc, ingctrl2.name)
		cmd := fmt.Sprintf("/usr/bin/env | grep %s", "ROUTER_BACKEND_CHECK_INTERVAL")
		_, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", podname2, "--", "bash", "-c", cmd).Output()
		o.Expect(err).To(o.HaveOccurred())
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-Low-50663-Negative Test of Make ROUTER_BACKEND_CHECK_INTERVAL Configurable", func() {
		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			ingctrl             = ingressControllerDescription{
				name:      "ocp50663",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("Create an custom ingresscontroller for testing ROUTER_BACKEND_CHECK_INTERVAL")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("Try to patch tuningOptions/healthCheckInterval 2147483900ms which is larger than the max healthCheckInterval, to the ingress-controller")
		NegHealthCheckInterval := "2147483900ms"
		ingctrlResource := "ingresscontrollers/" + ingctrl.name
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\": {\"tuningOptions\": {\"healthCheckInterval\": \""+NegHealthCheckInterval+"\"}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		compat_otp.By("Check ROUTER_BACKEND_CHECK_INTERVAL env in a route pod which should be the max: 2147483647ms")
		podname := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		hciSearch := readRouterPodEnv(oc, podname, "ROUTER_BACKEND_CHECK_INTERVAL")
		o.Expect(hciSearch).To(o.ContainSubstring("ROUTER_BACKEND_CHECK_INTERVAL=" + "2147483647ms"))

		compat_otp.By("Try to patch tuningOptions/healthCheckInterval -1s which is a minus value, to the ingress-controller")
		NegHealthCheckInterval = "-1s"
		output, err1 := oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", "{\"spec\": {\"tuningOptions\": {\"healthCheckInterval\": \""+NegHealthCheckInterval+"\"}}}", "--type=merge", "-n", ingctrl.namespace).Output()
		o.Expect(err1).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Invalid value: \"-1s\""))

		compat_otp.By("Try to patch tuningOptions/healthCheckInterval abc which is a string, to the ingress-controller")
		NegHealthCheckInterval = "0abc"
		output, err2 := oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", "{\"spec\": {\"tuningOptions\": {\"healthCheckInterval\": \""+NegHealthCheckInterval+"\"}}}", "--type=merge", "-n", ingctrl.namespace).Output()
		o.Expect(err2).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Invalid value: \"0abc\":"))
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-High-50926-Support a Configurable ROUTER_MAX_CONNECTIONS in HAproxy", func() {
		buildPruningBaseDir := compat_otp.FixturePath("testdata", "router")
		baseTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		extraParas := "    tuningOptions:\n      maxConnections: 2000\n"
		customTemp1 := addExtraParametersToYamlFile(baseTemp, "spec:", extraParas)
		defer os.Remove(customTemp1)
		ingctrl := ingressControllerDescription{
			name:      "ocp50926",
			namespace: "openshift-ingress-operator",
			domain:    "",
			template:  customTemp1,
		}
		ingctrlResource := "ingresscontrollers/" + ingctrl.name

		compat_otp.By("Create a custom ICs for testing router reload interval")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("Check ROUTER_MAX_CONNECTIONS env under a router pod of IC ocp50926, which should be 2000")
		podname := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		maxConnSearch := readRouterPodEnv(oc, podname, "ROUTER_MAX_CONNECTIONS")
		o.Expect(maxConnSearch).To(o.ContainSubstring("ROUTER_MAX_CONNECTIONS=2000"))

		compat_otp.By("Check maxconn in haproxy.config under a router pod of IC ocp50926, which should be 2000")
		maxConnCfg := readRouterPodData(oc, podname, "cat haproxy.config", "maxconn")
		o.Expect(maxConnCfg).To(o.ContainSubstring("maxconn 2000"))

		compat_otp.By("Patch tuningOptions/maxConnections with max 2000000 to IC ocp50926")
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\": {\"tuningOptions\": {\"maxConnections\": 2000000}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		compat_otp.By("Check ROUTER_MAX_CONNECTIONS env under a router pod of IC ocp50926,  which should be 2000000")
		podname = getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		maxConnSearch = readRouterPodEnv(oc, podname, "ROUTER_MAX_CONNECTIONS")
		o.Expect(maxConnSearch).To(o.ContainSubstring("ROUTER_MAX_CONNECTIONS=2000000"))

		compat_otp.By("Check maxconn in haproxy.config under a router pod of IC ocp50926, which should be 2000000")
		maxConnCfg = readRouterPodData(oc, podname, "cat haproxy.config", "maxconn")
		o.Expect(maxConnCfg).To(o.ContainSubstring("maxconn 2000000"))
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-High-50928-Negative test of Support a Configurable ROUTER_MAX_CONNECTIONS in HAproxy", func() {
		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-tuning.yaml")
			ingctrl             = ingressControllerDescription{
				name:      "ocp50928",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ingctrlResource = "ingresscontrollers/" + ingctrl.name
		)

		compat_otp.By("Create a custom IC with tuningOptions/maxConnections -1 specified by ingresscontroller-tuning.yaml")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("Check ROUTER_MAX_CONNECTIONS env under a route pod for the configured maxConnections -1,  which should be auto")
		podname := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		maxConnSearch := readRouterPodEnv(oc, podname, "ROUTER_MAX_CONNECTIONS")
		o.Expect(maxConnSearch).To(o.ContainSubstring("ROUTER_MAX_CONNECTIONS=auto"))

		compat_otp.By("Check maxconn in haproxy.config which won't appear after configured tuningOptions/maxConnections with -1")
		cmd := fmt.Sprintf("%s | grep \"%s\"", "cat haproxy.config", "maxconn")
		_, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", podname, "--", "bash", "-c", cmd).Output()
		o.Expect(err).To(o.HaveOccurred())

		compat_otp.By("Patch tuningOptions/maxConnections 0 to the IC")
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\": {\"tuningOptions\": {\"maxConnections\": 0}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		compat_otp.By("Try to Check ROUTER_MAX_CONNECTIONS env in a route pod set to the default maxConnections by 0")
		podname = getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		cmd = fmt.Sprintf("/usr/bin/env | grep %s", "ROUTER_MAX_CONNECTIONS")
		_, err = oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", podname, "--", "bash", "-c", cmd).Output()
		o.Expect(err).To(o.HaveOccurred())

		compat_otp.By("Check maxconn in haproxy.config under a router pod of the IC , which should be 50000")
		maxConnCfg := readRouterPodData(oc, podname, "cat haproxy.config", "maxconn")
		o.Expect(maxConnCfg).To(o.ContainSubstring("maxconn 50000"))

		compat_otp.By("Try to patch the ingress-controller with tuningOptions/maxConnections 1999, which is less than the min 2000")
		NegMaxConnections := "1999"
		output, err2 := oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", "{\"spec\": {\"tuningOptions\": {\"maxConnections\": "+NegMaxConnections+"}}}", "--type=merge", "-n", ingctrl.namespace).Output()
		o.Expect(err2).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Unsupported value: " + NegMaxConnections + ": supported values: \"-1\", \"0\""))

		compat_otp.By("Try to patch the ingress-controller with tuningOptions/maxConnections 2000001, which is a larger than the max 2000000")
		NegMaxConnections = "2000001"
		output, err2 = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", "{\"spec\": {\"tuningOptions\": {\"maxConnections\": "+NegMaxConnections+"}}}", "--type=merge", "-n", ingctrl.namespace).Output()
		o.Expect(err2).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Unsupported value: " + NegMaxConnections + ": supported values: \"-1\", \"0\""))

		compat_otp.By("Try to patch the ingress-controller with tuningOptions/maxConnections abc, which is a string")
		NegMaxConnections = "abc"
		output, err2 = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", "{\"spec\": {\"tuningOptions\": {\"maxConnections\": \""+NegMaxConnections+"\"}}}", "--type=merge", "-n", ingctrl.namespace).Output()
		o.Expect(err2).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Invalid value: \"string\": spec.tuningOptions.maxConnections in body must be of type integer"))
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-High-53605-Expose a Configurable Reload Interval in HAproxy", func() {
		buildPruningBaseDir := compat_otp.FixturePath("testdata", "router")
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

		compat_otp.By("Create two custom ICs for testing router reload interval")
		baseDomain := getBaseDomain(oc)
		ingctrl1.domain = ingctrl1.name + "." + baseDomain
		ingctrl2.domain = ingctrl2.name + "." + baseDomain
		defer ingctrl1.delete(oc)
		ingctrl1.create(oc)
		defer ingctrl2.delete(oc)
		ingctrl2.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl1.name, "1")
		ensureRouterDeployGenerationIs(oc, ingctrl2.name, "1")

		compat_otp.By("Check RELOAD_INTERVAL env in a route pod of IC ocp53605one, which should be 15s")
		podname1 := getOneNewRouterPodFromRollingUpdate(oc, ingctrl1.name)
		riSearch := readRouterPodEnv(oc, podname1, "RELOAD_INTERVAL")
		o.Expect(riSearch).To(o.ContainSubstring("RELOAD_INTERVAL=15s"))

		compat_otp.By("Check RELOAD_INTERVAL env in a route pod of IC ocp53605two, which should be 2m")
		podname2 := getOneNewRouterPodFromRollingUpdate(oc, ingctrl2.name)
		riSearch = readRouterPodEnv(oc, podname2, "RELOAD_INTERVAL")
		o.Expect(riSearch).To(o.ContainSubstring("RELOAD_INTERVAL=2m"))

		compat_otp.By("Patch tuningOptions/reloadInterval with other valid unit m, for exmpale 1m to IC ocp53605one, while patch it with 0s to IC ocp53605two")
		patchResourceAsAdmin(oc, ingctrl1.namespace, ingctrlResource1, "{\"spec\": {\"tuningOptions\": {\"reloadInterval\": \"1m\"}}}")
		patchResourceAsAdmin(oc, ingctrl2.namespace, ingctrlResource2, "{\"spec\": {\"tuningOptions\": {\"reloadInterval\": \"0s\"}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl1.name, "2")
		ensureRouterDeployGenerationIs(oc, ingctrl2.name, "2")

		compat_otp.By("Check RELOAD_INTERVAL env in a route pod of IC ocp53605one which should be 1m")
		podname1 = getOneNewRouterPodFromRollingUpdate(oc, ingctrl1.name)
		riSearch = readRouterPodEnv(oc, podname1, "RELOAD_INTERVAL")
		o.Expect(riSearch).To(o.ContainSubstring("RELOAD_INTERVAL=1m"))

		compat_otp.By("Check RELOAD_INTERVAL env in a route pod of IC ocp53605two, which is the default 5s")
		podname2 = getOneNewRouterPodFromRollingUpdate(oc, ingctrl2.name)
		riSearch = readRouterPodEnv(oc, podname2, "RELOAD_INTERVAL")
		o.Expect(riSearch).To(o.ContainSubstring("RELOAD_INTERVAL=5s"))
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-Low-53608-Negative Test of Expose a Configurable Reload Interval in HAproxy", func() {
		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			ingctrl             = ingressControllerDescription{
				name:      "ocp53608",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("Create an custom ingresscontroller for testing router reload interval")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("Try to patch tuningOptions/reloadInterval 121s which is larger than the max 120s, to the ingress-controller")
		NegReloadInterval := "121s"
		ingctrlResource := "ingresscontrollers/" + ingctrl.name
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\": {\"tuningOptions\": {\"reloadInterval\": \""+NegReloadInterval+"\"}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		compat_otp.By("Check RELOAD_INTERVAL env in a route pod which should be the max 2m")
		podname := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		riSearch := readRouterPodEnv(oc, podname, "RELOAD_INTERVAL")
		o.Expect(riSearch).To(o.ContainSubstring("RELOAD_INTERVAL=2m"))

		compat_otp.By("Try to patch tuningOptions/reloadInterval 0.5s which is less than the min 1s, to the ingress-controller")
		NegReloadInterval = "0.5s"
		ingctrlResource = "ingresscontrollers/" + ingctrl.name
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\": {\"tuningOptions\": {\"reloadInterval\": \""+NegReloadInterval+"\"}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "3")

		compat_otp.By("Check RELOAD_INTERVAL env in a route pod which should be the min 1s")
		podname = getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		riSearch = readRouterPodEnv(oc, podname, "RELOAD_INTERVAL")
		o.Expect(riSearch).To(o.ContainSubstring("RELOAD_INTERVAL=1s"))

		compat_otp.By("Try to patch tuningOptions/reloadInterval -1s which is a minus value, to the ingress-controller")
		NegReloadInterval = "-1s"
		output, errCfg := patchResourceAsAdminAndGetLog(oc, ingctrl.namespace, ingctrlResource, "{\"spec\": {\"tuningOptions\": {\"reloadInterval\": \""+NegReloadInterval+"\"}}}")
		o.Expect(errCfg).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Invalid value: \"" + NegReloadInterval + "\""))

		compat_otp.By("Try to patch tuningOptions/reloadInterval 1abc which is a string, to the ingress-controller")
		NegReloadInterval = "1abc"
		output, errCfg = patchResourceAsAdminAndGetLog(oc, ingctrl.namespace, ingctrlResource, "{\"spec\": {\"tuningOptions\": {\"reloadInterval\": \""+NegReloadInterval+"\"}}}")
		o.Expect(errCfg).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Invalid value: \"" + NegReloadInterval + "\""))

		compat_otp.By("Try to patch tuningOptions/reloadInterval 012 s which contains a space character, to the ingress-controller")
		NegReloadInterval = "012 s"
		output, errCfg = patchResourceAsAdminAndGetLog(oc, ingctrl.namespace, ingctrlResource, "{\"spec\": {\"tuningOptions\": {\"reloadInterval\": \""+NegReloadInterval+"\"}}}")
		o.Expect(errCfg).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Invalid value: \"" + NegReloadInterval + "\""))
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-LEVEL0-High-55367-Default HAProxy maxconn value to 50000 for OCP 4.12 and later", func() {
		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
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
		maxConnCfg := readRouterPodData(oc, podname, "cat haproxy.config", "maxconn")
		o.Expect(maxConnCfg).To(o.ContainSubstring("maxconn 50000"))

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
		maxConnCfg = readRouterPodData(oc, podname, "cat haproxy.config", "maxconn")
		o.Expect(maxConnCfg).To(o.ContainSubstring("maxconn 50000"))

		compat_otp.By("Patch tuningOptions/maxConnections 50000 to the ingress-controller")
		maxConnections = "500000"
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\": {\"tuningOptions\": {\"maxConnections\": "+maxConnections+"}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		compat_otp.By("Check ROUTER_MAX_CONNECTIONS env in a route pod which should be " + maxConnections)
		podname = getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		maxConnSearch := readRouterPodEnv(oc, podname, "ROUTER_MAX_CONNECTIONS")
		o.Expect(maxConnSearch).To(o.ContainSubstring("ROUTER_MAX_CONNECTIONS=" + maxConnections))

		compat_otp.By("Check maxconn in haproxy.config which should be 50000")
		maxConnCfg = readRouterPodData(oc, podname, "cat haproxy.config", "maxconn")
		o.Expect(maxConnCfg).To(o.ContainSubstring("maxconn 50000"))
	})
})
