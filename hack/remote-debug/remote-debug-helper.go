// This program dynamically updates /etc/profile.d/router-default.sh to
// include environment variables from the 'router-default' deployment
// in the 'openshift-ingress' namespace. By doing so, it mimics the
// environment available through 'oc rsh <router-pod>', simplifying
// the debugging process and eliminating the need to manually specify
// numerous options when running openshift-router.
//
// The utility synchronises the environment variables by monitoring
// changes in the 'router-default' deployment, ensuring that any SSH
// session provides an accurate context for troubleshooting and
// interacting with the router as if commands were executed directly
// within the pod.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	v1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

const (
	deploymentName = "router-default"
	namespace      = "openshift-ingress"
)

var lastEnvContent string

func main() {
	var envFilePath string

	// Parse the command-line arguments.
	flag.StringVar(&envFilePath, "env-file", "/etc/profile.d/router-default.sh", "Path to the environment file")
	flag.Parse()

	config, err := rest.InClusterConfig()
	if err != nil {
		// Fallback to local config.
		if home := homedir.HomeDir(); home != "" {
			configPath := filepath.Join(home, ".kube", "config")
			config, err = clientcmd.BuildConfigFromFlags("", configPath)
			if err != nil {
				fmt.Printf("Error creating local config: %v\n", err)
				os.Exit(1)
			}
		} else {
			fmt.Printf("Error creating in-cluster config: %v\n", err)
			os.Exit(1)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		fmt.Printf("Error creating Kubernetes client: %v\n", err)
		os.Exit(1)
	}

	// Create a list watcher for the deployments.
	deploymentListWatcher := cache.NewListWatchFromClient(
		clientset.AppsV1().RESTClient(),
		"deployments",
		namespace,
		fields.Everything(),
	)

	// Create an informer.
	informer := cache.NewSharedIndexInformer(
		deploymentListWatcher,
		&v1.Deployment{},
		5*time.Second,
		cache.Indexers{},
	)

	// Add event handlers to the informer.
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			deployment := obj.(*v1.Deployment)
			if deployment.Name == deploymentName {
				writeEnvFile(deployment, "AddFunc", envFilePath, clientset)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			deployment := newObj.(*v1.Deployment)
			if deployment.Name == deploymentName {
				writeEnvFile(deployment, "UpdateFunc", envFilePath, clientset)
			}
		},
		DeleteFunc: func(obj interface{}) {
			deployment := obj.(*v1.Deployment)
			if deployment.Name == deploymentName {
				fmt.Printf("Deployment %s/%s deleted. Event: DeleteFunc\n", namespace, deploymentName)
			}
		},
	})

	stopCh := make(chan struct{})
	defer close(stopCh)

	go informer.Run(stopCh)

	// Wait for signals to stop the program.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("Shutting down...")
}

func writeEnvFile(deployment *v1.Deployment, event, envFilePath string, clientset *kubernetes.Clientset) {
	envFileContent := extractEnvVars(deployment, clientset)

	if envFileContent == lastEnvContent {
		// fmt.Printf("No changes in environment variables. Skipping file write. Event: %s\n", event)
		return
	}

	err := os.WriteFile(envFilePath, []byte(envFileContent), 0644)
	if err != nil {
		fmt.Printf("Error writing to file: %v\n", err)
		os.Exit(1)
	}

	lastEnvContent = envFileContent
	fmt.Printf("Deployment %s/%s environment variables written to %s. Event: %s\n", namespace, deploymentName, envFilePath, event)
}

func extractEnvVars(deployment *v1.Deployment, clientset *kubernetes.Clientset) string {
	var envFileContent string

	// Extract environment variables from the deployment.
	for _, container := range deployment.Spec.Template.Spec.Containers {
		for _, env := range container.Env {
			envFileContent += fmt.Sprintf("export %s=%s\n", env.Name, env.Value)
		}
	}

	// These are specified in the Dockerfile as explicit ENV
	// variables.
	envFileContent += "export TEMPLATE_FILE=/var/lib/haproxy/conf/haproxy-config.template\n"
	envFileContent += "export RELOAD_SCRIPT=/var/lib/haproxy/reload-haproxy\n"

	// QoL improvement when debugging.
	envFileContent += "export ROUTER_GRACEFUL_SHUTDOWN_DELAY=0s\n"

	// Add Kubernetes environment variables dynamically.
	envFileContent += generateKubernetesEnvVars(clientset)

	return envFileContent
}

func generateKubernetesEnvVars(clientset *kubernetes.Clientset) string {
	var envVars []string

	// Get the list of services in the namespace.
	services, err := clientset.CoreV1().Services(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		fmt.Printf("Error listing services: %v\n", err)
		return ""
	}

	for _, service := range services.Items {
		prefix := strings.ToUpper(service.Name) + "_SERVICE"
		prefix = strings.ReplaceAll(prefix, "-", "_")
		host := fmt.Sprintf("%s_HOST=%s", prefix, service.Spec.ClusterIP)
		port := fmt.Sprintf("%s_PORT=%d", prefix, service.Spec.Ports[0].Port)
		envVars = append(envVars, fmt.Sprintf("export %s", host))
		envVars = append(envVars, fmt.Sprintf("export %s", port))
	}

	// Add the Kubernetes service environment variables.
	envVars = append(envVars,
		"export KUBERNETES_SERVICE_HOST=172.30.0.1",
		"export KUBERNETES_SERVICE_PORT=443",
	)

	return strings.Join(envVars, "\n")
}
