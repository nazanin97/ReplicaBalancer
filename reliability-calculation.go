package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

var prometheusURL = "http://localhost:9090"
var prometheusAPI v1.API

type PodMetrics struct {
	restarts            float64
	averageResponseTime float64
	memoryConsumption   float64
}

var (
	restartWeight      = 0.5
	responseTimeWeight = 0.2
	memoryWeight       = 0.3
)

func setPrometheus(ip string) {
	prometheusURL = "http://" + ip + ":9090"
	fmt.Printf("Prometheus IP: %s\n", prometheusURL)

	client, err := api.NewClient(api.Config{
		Address: prometheusURL,
	})
	if err != nil {
		log.Fatalf("Error creating client: %v\n", err)
	}

	prometheusAPI = v1.NewAPI(client)
}

func findMinMax(nums []float64) (float64, float64) {
	min := math.MaxFloat64
	max := -math.MaxFloat64

	for _, value := range nums {
		if value < min {
			min = value
		}
		if value > max {
			max = value
		}
	}
	return min, max
}

func standardDeviation(values []float64, metric string) float64 {

	// Calculate the mean
	mean := average(values, metric)

	// Calculate the variance
	variance := 0.0
	for _, value := range values {
		difference := value - mean
		variance += difference * difference
	}
	variance /= float64(len(values))
	// Standard deviation is the square root of the variance
	return math.Sqrt(variance)
}

func average(nums []float64, metric string) float64 {
	if len(nums) == 0 {
		return 0
	}
	total := 0.0
	count := 0.0
	for _, num := range nums {
		if num == 0.0 && metric != "restarts" {
			continue // Skip zero values for averageResponseTime and memoryUsage metrics as they are not valid
		}
		total += num
		count++
	}

	// Check if count is zero (all values were zero)
	if count == 0 {
		return 0
	}
	return total / count
}

func readAndNormalizeMetrics(deploymentVersions []DeploymentVersion, metricName string) []float64 {

	metricValues := make([]float64, len(deploymentVersions))

	for i, deploymentVersion := range deploymentVersions {
		deploymentMetricsData := readMetricsData(deploymentVersion.DeploymentName)
		if metricName == "averageResponseTime" {
			metricValues[i] = standardDeviation(deploymentMetricsData[metricName], metricName)
		} else if metricName == "memoryUsage" {
			metricValues[i] = standardDeviation(deploymentMetricsData[metricName], metricName)
		} else {
			metricValues[i] = average(deploymentMetricsData[metricName], metricName)
		}
	}

	min, max := findMinMax(metricValues)

	// if the difference is not too much, set normalized value the same for all. we assume 5MB is not too much
	if metricName == "memoryUsage" && math.Abs(max-min) < 5.0 {
		min = max
	}
	// if the difference is not too much, set normalized value the same for all. we assume 0.1s is not too much
	if metricName == "averageResponseTime" && math.Abs(max-min) < 0.1 {
		min = max
	}

	// Normalize values linearly between 0 and 1.
	for i, value := range metricValues {
		if max != min {
			metricValues[i] = 1 - (value-min)/(max-min)
		} else {
			metricValues[i] = 1
		}
	}
	return metricValues
}

func calculateReliabilityScore(restartScore float64, responseTimeScore float64, memoryScore float64) float64 {
	reliabilityScore := responseTimeWeight*responseTimeScore + restartWeight*restartScore + memoryWeight*memoryScore
	return reliabilityScore
}

func fetchMetricData(client v1.API, query string) (float64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()
	result, warnings, err := client.Query(ctx, query, time.Now())

	if err != nil {
		return 0, err
	}
	if len(warnings) > 0 {
		fmt.Printf("Warnings: %v\n", warnings)
	}

	vector := result.(model.Vector)
	if len(vector) > 0 {
		return float64(vector[0].Value), nil
	}
	return 0, nil
}

func getPrometheusData(nameSpace string, deploymentName string) {

	pod := PodMetrics{}
	var err error
	pod.restarts, err = fetchMetricData(prometheusAPI, fmt.Sprintf("sum(kube_pod_container_status_restarts_total{namespace=\"%s\", pod=~\"%s.*\"} * on(pod) group_left kube_pod_status_phase{namespace=\"%s\", phase=\"Running\"})", nameSpace, deploymentName, nameSpace))
	if err != nil {
		log.Fatalf("Error querying Prometheus: %v\n", err)
	}

	// Fetch total memory consumption
	pod.memoryConsumption, err = fetchMetricData(prometheusAPI, fmt.Sprintf("sum(container_memory_usage_bytes{namespace='%s', pod=~'%s.*'} * on (pod, namespace) group_left(phase) kube_pod_status_phase{phase='Running'})", nameSpace, deploymentName))
	if err != nil {
		log.Fatalf("Error querying Prometheus: %v\n", err)
	}

	// Fetch the count of running pods
	podCount, err := fetchMetricData(prometheusAPI, fmt.Sprintf("count(kube_pod_status_phase{namespace='%s', pod=~'%s.*', phase='Running'})", nameSpace, deploymentName))
	if err != nil {
		log.Fatalf("Error querying Prometheus: %v\n", err)
	}

	// Compute average memory usage by dividing total memory consumption by the number of running pods.
	if podCount == 0.0 {
		podCount = 1.0 // Avoid division by zero
	}
	// Conversion from bytes to Megabytes.
	pod.memoryConsumption = (pod.memoryConsumption / 1024 / 1024) / podCount

	serviceData, err := getServiceIPAndPort(deploymentName + "-service")
	if err != nil {
		fmt.Printf("Error fetching service details: %v\n", err)
	}

	avg, err := readLogs(fmt.Sprintf("%s:%d", serviceData.ServiceIP, serviceData.ServicePort))

	if err != nil {
		fmt.Println("Error:", err)
		pod.averageResponseTime = 0
	} else {
		pod.averageResponseTime = avg
	}

	writeMetricsData(deploymentName, float64(pod.restarts), float64(pod.averageResponseTime), float64(pod.memoryConsumption))
}
