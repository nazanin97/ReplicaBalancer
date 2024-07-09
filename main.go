package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apimachineryerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/pointer"
)

// ENV Variables
const versionsEnvVar = "DEPLOYMENT_IMAGES_REPLICAS"
const monitoringTimeEnvVar = "MONITORING_TIME"
const totalReplicasEnvVar = "TOTAL_REPLICAS"
const maxReplicaEnvVar = "MAX_REPLICAS"
const minReplicaEnvVar = "MIN_REPLICAS"
const cpuMaxEnvVar = "MAX_CPU"
const cpuMinEnvVar = "MIN_CPU"
const scaleEnvVar = "SCALING"

// Prometheus
const prometheus_namespace = "monitoring"
const prometheus_service_name = "prometheus-kube-prometheus-prometheus"

// Directory to save files
const dataFileDirectory = "/var/data"

// Ports for services for each software version
var servicePorts = [4]int32{8010, 8011, 8012, 8013}
var serviceNodePorts = [4]int32{30000, 30001, 30002, 30003}
var testDeploymentNames = [4]string{"faulty", "inconsistent-response", "memory-leak", "healthy"}

var indexPort = 0

var reliability_scores = []float64{}

var (
	MIN_REPLICAS = 3
	MAX_REPLICAS = 24
)

var total_replicas = 9 //total current replicas of system
var do_scale = false

var clientset *kubernetes.Clientset

const namespace = "default"

type DeploymentVersion struct {
	ImageName      string
	DeploymentName string
	ContainerName  string
	Replicas       int32
	Score          int
}

type NginxServer struct {
	Server string `json:"server"`
	Port   int32  `json:"port"`
	Weight int    `json:"weight"`
}

type ServiceData struct {
	ServiceIP   string
	ServicePort int32
}

func getServiceIPAndPort(serviceName string) (*ServiceData, error) {
	service, err := clientset.CoreV1().Services(namespace).Get(context.Background(), serviceName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("Failed to get service %s: %v", serviceName, err)
	}

	// Check if the service has an external IP
	var serviceIP string
	var servicePort int32
	if len(service.Status.LoadBalancer.Ingress) > 0 {
		serviceIP = service.Status.LoadBalancer.Ingress[0].IP
		servicePort = service.Spec.Ports[0].NodePort
	} else {
		// If no external IP is assigned, use the cluster IP
		serviceIP = service.Spec.ClusterIP
		servicePort = service.Spec.Ports[0].Port
	}

	return &ServiceData{ServiceIP: serviceIP, ServicePort: servicePort}, nil
}

func main() {

	// Parse the environment variable for monitoring delay time
	monitoringDelayEnvVar := os.Getenv(monitoringTimeEnvVar)
	sleepDuration := 30 * time.Second // default value
	if monitoringDelayEnvVar != "" {
		parsedDuration, err := time.ParseDuration(monitoringDelayEnvVar)
		if err != nil {
			log.Printf("Invalid duration: %v", err)
		} else {
			sleepDuration = parsedDuration
		}
	}

	// Parse the environment variable for total number of replicas
	if os.Getenv(totalReplicasEnvVar) != "" {
		total_replicas, _ = strconv.Atoi(os.Getenv(totalReplicasEnvVar))
	}

	// Parse the environment variable for max replicas
	if os.Getenv(maxReplicaEnvVar) != "" {
		MAX_REPLICAS, _ = strconv.Atoi(os.Getenv(maxReplicaEnvVar))
	}

	// Parse the environment variable for min replicas
	if os.Getenv(minReplicaEnvVar) != "" {
		MIN_REPLICAS, _ = strconv.Atoi(os.Getenv(minReplicaEnvVar))
	}

	// Parse the environment variable for min replicas
	if os.Getenv(cpuMaxEnvVar) != "" {
		CPU_max_threshold, _ = strconv.Atoi(os.Getenv(cpuMaxEnvVar))
	}

	// Parse the environment variable for min replicas
	if os.Getenv(cpuMinEnvVar) != "" {
		CPU_min_threshold, _ = strconv.Atoi(os.Getenv(cpuMinEnvVar))
	}

	if scaleStr := os.Getenv(scaleEnvVar); scaleStr != "" {
		var err error
		do_scale, err = strconv.ParseBool(scaleStr)
		if err != nil {
			fmt.Printf("Error parsing %s as boolean: %v\n", scaleStr, err)
			do_scale = false
		}
	}

	// Parse the environment variable for deployment images and replicas
	versionsEnvVar := os.Getenv(versionsEnvVar)
	if versionsEnvVar == "" {
		fmt.Println("Please provide the DEPLOYMENT_IMAGES_REPLICAS.")
		return
	}

	// Set up Kubernetes client
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err)
	}

	clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}

	//create directory for writing in the files
	err = os.MkdirAll(dataFileDirectory, os.ModePerm)
	if err != nil {
		fmt.Printf("Failed to create directory: %v\n", err)
		return
	}

	deploymentVersions := parseDeploymentVersions(versionsEnvVar)
	if deploymentVersions == nil {
		fmt.Println("Please provide the DEPLOYMENT_IMAGES_REPLICAS in the correct format.")
		return
	}

	// Create deployments
	for _, deploymentVersion := range deploymentVersions {
		createDeployment(deploymentVersion)
	}

	// Get the Prometheus service
	service, err := clientset.CoreV1().Services(prometheus_namespace).Get(context.TODO(), prometheus_service_name, metav1.GetOptions{})
	if err != nil {
		fmt.Printf("Failed to get prometheus service: %v\n", err)
		return
	}
	// Check if the service has an external IP address
	if service.Spec.Type == corev1.ServiceTypeLoadBalancer && len(service.Status.LoadBalancer.Ingress) > 0 {
		externalIP := service.Status.LoadBalancer.Ingress[0].IP
		if externalIP != "" {
			setPrometheus(externalIP)
		} else {
			fmt.Println("No external IP address found for Prometheus LoadBalancer service, using Cluster IP instead")
			setPrometheus(service.Spec.ClusterIP)
		}
	} else {
		fmt.Println("No external IP address found for Prometheus LoadBalancer service, using Cluster IP instead")
		setPrometheus(service.Spec.ClusterIP)
	}

	//config nginx
	servers := make([]NginxServer, len(deploymentVersions))
	for index, deploymentVersion := range deploymentVersions {

		// Fetch service details
		serviceData, err := getServiceIPAndPort(deploymentVersion.DeploymentName + "-service")
		if err != nil {
			fmt.Printf("Error fetching service details: %v\n", err)
			continue
		}

		// Create server data
		servers[index] = NginxServer{
			Server: serviceData.ServiceIP,
			Port:   serviceData.ServicePort,
			Weight: int(1),
		}
	}
	nginxMain(clientset, servers)

	time.Sleep(10 * time.Second)

	// Monitor and calculate reliability score
	monitoringTicker := time.NewTicker(sleepDuration)
	reliabilityCalculationTicker := time.NewTicker(4 * sleepDuration)

	var currentCPUValue float64
	var scalingHistory []int

	for {
		select {
		case <-monitoringTicker.C:
			if do_scale {
				currentCPUValue = getCPU()
				currentScalingState := determineScalingAction(currentCPUValue)
				// Store the current scaling state
				scalingHistory = append(scalingHistory, currentScalingState)
				if len(scalingHistory) > 4 { // Keep only the last 4 entries
					scalingHistory = scalingHistory[1:]
				}
			}

			for _, deploymentVersion := range deploymentVersions {
				getPrometheusData(namespace, deploymentVersion.DeploymentName)
			}
		case <-reliabilityCalculationTicker.C:

			if do_scale {
				actualScalingAction := decideScalingBasedOnHistory(scalingHistory)

				if actualScalingAction == Increase && total_replicas < MAX_REPLICAS {
					total_replicas++
					fmt.Printf("Max total replicas increased = %v\n", total_replicas)
				} else if actualScalingAction == Decrease && total_replicas > MIN_REPLICAS {
					total_replicas--
					fmt.Printf("Max total replicas decreased = %v\n", total_replicas)
				}
			}

			reliability_scores = []float64{}

			restartMetrics := readAndNormalizeMetrics(deploymentVersions, "restarts")
			responseTimeMetrics := readAndNormalizeMetrics(deploymentVersions, "averageResponseTime")
			memoryMetrics := readAndNormalizeMetrics(deploymentVersions, "memoryUsage")

			for index, deploymentVersion := range deploymentVersions {
				reliabilityScore := calculateReliabilityScore(restartMetrics[index], responseTimeMetrics[index], memoryMetrics[index])
				fmt.Printf("The reliability score for %s is: %v\n\n", deploymentVersion.DeploymentName, reliabilityScore)
				reliability_scores = append(reliability_scores, float64(reliabilityScore))
			}

			scaleDeployment(deploymentVersions)

		}
	}
}

func parseDeploymentVersions(envVar string) []DeploymentVersion {
	match, _ := regexp.MatchString(`^([a-zA-Z0-9\-_./]+:[a-zA-Z0-9\-_.]+(\*\d*)?,)*[a-zA-Z0-9\-_./]+:[a-zA-Z0-9\-_.]+(\*\d*)?$`, envVar)
	if !match {
		return nil
	}
	deploymentVersions := []DeploymentVersion{}

	// The environment variable is in format "image1:replicas1,image2:replicas2,..."
	versions := strings.Split(envVar, ",")

	// First pass to count the versions without specified replicas
	noReplicaCount := 0
	for _, version := range versions {
		if !strings.Contains(version, "*") {
			noReplicaCount++
		}
	}
	defaultReplicas := total_replicas / noReplicaCount

	deploymentsClient := clientset.AppsV1().Deployments(metav1.NamespaceDefault)

	// Second pass to assign replicas
	for index, version := range versions {
		splitted := strings.Split(version, "*")
		imageName := splitted[0]

		//for testing online boutique
		name := "frontend-" + testDeploymentNames[index]

		var replicas int
		if len(splitted) > 1 && splitted[1] != "" {
			replicas, _ = strconv.Atoi(splitted[1])
		} else {

			deploymentName := name + "-deployment"
			deployment, err := deploymentsClient.Get(context.TODO(), deploymentName, metav1.GetOptions{})

			if err == nil {
				// Deployment exists, use its current replica count
				replicas = int(deployment.Status.Replicas)
			} else {
				replicas = defaultReplicas
			}
		}

		deploymentName := name + "-deployment"
		containerName := name + "-container"

		deploymentVersions = append(deploymentVersions, DeploymentVersion{
			ImageName:      imageName,
			DeploymentName: deploymentName,
			ContainerName:  containerName,
			Replicas:       int32(replicas),
		})

		writeToFile("/var/data/replicas.csv", deploymentName, []float64{float64(replicas), 1}, "replicas,reliability-score")
	}
	return deploymentVersions
}

func createDeployment(deploymentV DeploymentVersion) {

	deploymentsClient := clientset.AppsV1().Deployments(namespace)

	// Get the current deployment
	currentDeployment, err := deploymentsClient.Get(context.TODO(), deploymentV.DeploymentName, metav1.GetOptions{})

	if err != nil && !apimachineryerrors.IsNotFound(err) {
		log.Printf("Failed to get deployment %s: %v", deploymentV.DeploymentName, err)
	}

	// If the current deployment exists and the image tag is different, delete the deployment
	if err == nil {
		currentImage := currentDeployment.Spec.Template.Spec.Containers[0].Image

		if currentImage != deploymentV.ImageName {
			deploymentV.Replicas = *currentDeployment.Spec.Replicas

			err = deploymentsClient.Delete(context.TODO(), deploymentV.DeploymentName, metav1.DeleteOptions{})
			if err != nil {
				fmt.Printf("Failed to delete old deployment %s: %v\n", deploymentV.DeploymentName, err)
			} else {
				fmt.Printf("Old deployment %s deleted\n", deploymentV.DeploymentName)
				resetMetricsData(deploymentV.DeploymentName)
			}
		}
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: deploymentV.DeploymentName,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &deploymentV.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": deploymentV.DeploymentName},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": deploymentV.DeploymentName},
					Annotations: map[string]string{
						"sidecar.istio.io/rewriteAppHTTPProbers": "true",
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: "default",
					SecurityContext: &corev1.PodSecurityContext{
						FSGroup:      pointer.Int64Ptr(1000),
						RunAsGroup:   pointer.Int64Ptr(1000),
						RunAsNonRoot: pointer.BoolPtr(true),
						RunAsUser:    pointer.Int64Ptr(1000),
					},
					Containers: []corev1.Container{
						{
							Name:  deploymentV.ContainerName,
							Image: deploymentV.ImageName,
							Ports: []corev1.ContainerPort{{ContainerPort: 8080}},
							Env: []corev1.EnvVar{
								{Name: "PORT", Value: "8080"},
								{Name: "PRODUCT_CATALOG_SERVICE_ADDR", Value: "productcatalogservice:3550"},
								{Name: "CURRENCY_SERVICE_ADDR", Value: "currencyservice:7000"},
								{Name: "CART_SERVICE_ADDR", Value: "cartservice:7070"},
								{Name: "RECOMMENDATION_SERVICE_ADDR", Value: "recommendationservice:8080"},
								{Name: "SHIPPING_SERVICE_ADDR", Value: "shippingservice:50051"},
								{Name: "CHECKOUT_SERVICE_ADDR", Value: "checkoutservice:5050"},
								{Name: "AD_SERVICE_ADDR", Value: "adservice:9555"},
								{Name: "ENABLE_PROFILER", Value: "0"},
							},
							ImagePullPolicy: corev1.PullAlways,
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: pointer.BoolPtr(false),
								Privileged:               pointer.BoolPtr(false),
								ReadOnlyRootFilesystem:   pointer.BoolPtr(true),
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("50m"),
									corev1.ResourceMemory: resource.MustParse("64Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
							},
						},
					},
				},
			},
		},
	}

	// Create the deployment
	_, err = clientset.AppsV1().Deployments(namespace).Create(context.Background(), deployment, metav1.CreateOptions{})
	if err != nil {
		log.Printf("Failed to create deployment %s: %v", deploymentV.DeploymentName, err)
	}

	// Create the service
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: deploymentV.DeploymentName + "-service",
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": deploymentV.DeploymentName},
			Ports: []corev1.ServicePort{
				{
					Protocol:   corev1.ProtocolTCP,
					Port:       int32(servicePorts[indexPort]),
					TargetPort: intstr.FromInt(8080),
					NodePort:   int32(serviceNodePorts[indexPort]),
				},
			},
			Type: corev1.ServiceTypeNodePort,
		},
	}

	_, err = clientset.CoreV1().Services(namespace).Create(context.Background(), service, metav1.CreateOptions{})
	if err != nil {
		log.Printf("Failed to create service %s: %v", deploymentV.DeploymentName, err)
	}
	indexPort = indexPort + 1
}

func writeToFile(filePath string, deploymentName string, rawData []float64, metrics string) {

	// Get the current time
	currentTime := time.Now().Format(time.RFC3339)
	data := fmt.Sprintf("%s,%s", currentTime, deploymentName)
	for _, value := range rawData {
		data += fmt.Sprintf(",%v", value)
	}
	data += "\n"

	// Create the CSV file if it doesn't exist and open it in append mode
	file, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("Failed to open file: %v\n", err)
		return
	}
	defer file.Close()

	// Write the header row if the file is empty
	fileInfo, _ := file.Stat()
	isEmpty := fileInfo.Size() == 0

	if isEmpty {
		header := fmt.Sprintf("time,deployment-name,%s\n", metrics)
		_, err = file.WriteString(header)
		if err != nil {
			fmt.Printf("Failed to write header to file: %v\n", err)
			return
		}
	}

	// Write the content to the file
	_, err = file.WriteString(data)
	if err != nil {
		fmt.Printf("Failed to write to file: %v\n", err)
		return
	}
}

func scaleDeployment(deploymentVersions []DeploymentVersion) {

	total_score := 0.0
	for _, score := range reliability_scores {
		total_score += score
	}

	newReplicas := make([]int32, len(reliability_scores))
	fractionalParts := make([]float64, len(reliability_scores))
	totalReplicas := 0
	for index, score := range reliability_scores {

		proportionalReplica := float64(total_replicas) * score / total_score
		newReplicas[index] = int32(proportionalReplica)
		fractionalParts[index] = proportionalReplica - float64(newReplicas[index])

		// Ensure minimum replica is 1
		if newReplicas[index] == 0 {
			newReplicas[index] = 1
		}
		totalReplicas += int(newReplicas[index])
	}

	// Sort the indices by their fractional parts in descending order
	indices := make([]int, 0, len(fractionalParts))
	for index := range fractionalParts {
		indices = append(indices, index)
	}
	sort.Slice(indices, func(i, j int) bool {
		return fractionalParts[indices[i]] > fractionalParts[indices[j]]
	})

	// Distribute or remove the remaining replicas
	difference := totalReplicas - total_replicas
	if difference < 0 {
		// There are replicas to distribute
		for i := 0; i < -difference; i++ {
			newReplicas[indices[i]]++
		}
	} else if difference > 0 {
		// There are too many replicas, need to remove some
		// Start from the end of the indices slice to remove from deployments with the smallest fractional parts
		for i := len(indices) - 1; i >= len(indices)-difference; i-- {
			if newReplicas[indices[i]] > 1 { // Ensure we're not reducing a deployment to 0 replicas
				newReplicas[indices[i]]--
			}
		}
	}

	fmt.Println("new replicas:", newReplicas)

	deploymentsClient := clientset.AppsV1().Deployments(metav1.NamespaceDefault)

	do_scaling := false
	for index, deploymentVersion := range deploymentVersions {

		// Retrieve the existing deployment
		deployment, err := deploymentsClient.Get(context.TODO(), deploymentVersion.DeploymentName, metav1.GetOptions{})
		if err != nil {
			log.Printf("Failed to get deployment %s: %v", deploymentVersion.DeploymentName, err)
			return
		}

		// Check if the difference is more than 5% of total do the scaling
		currentReplicaCount := *deployment.Spec.Replicas
		absoluteDifference := math.Abs(float64(newReplicas[index]) - float64(currentReplicaCount))
		percentageDifference := 100 * (absoluteDifference / float64(total_replicas))

		if percentageDifference >= 5.0 {
			do_scaling = true
			fmt.Printf("scaling...\n")
			break
		}
	}

	if do_scaling {
		servers := make([]NginxServer, len(deploymentVersions))
		for index, deploymentVersion := range deploymentVersions {

			// Fetch service details
			serviceData, err := getServiceIPAndPort(deploymentVersion.DeploymentName + "-service")
			if err != nil {
				fmt.Printf("Error fetching service details: %v\n", err)
				continue
			}

			// Create server data
			servers[index] = NginxServer{
				Server: serviceData.ServiceIP,
				Port:   serviceData.ServicePort,
				Weight: int(newReplicas[index]),
			}

			// Retrieve the existing deployment
			deployment, err := deploymentsClient.Get(context.TODO(), deploymentVersion.DeploymentName, metav1.GetOptions{})
			if err != nil {
				log.Printf("Failed to get deployment %s: %v", deploymentVersion.DeploymentName, err)
				return
			}

			// Update the replica count
			deployment.Spec.Replicas = &newReplicas[index]

			// Update the deployment
			_, err = deploymentsClient.Update(context.TODO(), deployment, metav1.UpdateOptions{})
			if err != nil {
				log.Printf("Failed to update deployment %s: %v", deploymentVersion.DeploymentName, err)
				return
			}

			writeToFile("/var/data/replicas.csv", deploymentVersion.DeploymentName, []float64{float64(newReplicas[index]), reliability_scores[index]}, "replicas,reliability-score")
		}
		fmt.Println("**********")

		//change services weights in nginx config file
		nginxMain(clientset, servers)
	}
}
