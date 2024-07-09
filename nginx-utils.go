package main

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func nginxMain(clientset *kubernetes.Clientset, servers []NginxServer) {

	// Specify the namespace and ConfigMap name
	namespace := "default"
	configmapName := "nginx-conf"

	// Get the existing ConfigMap
	configMap, err := clientset.CoreV1().ConfigMaps(namespace).Get(context.TODO(), configmapName, metav1.GetOptions{})
	if err != nil {
		panic(err)
	}

	// Generate Nginx upstream configuration based on the servers
	upstreamConfig := make([]string, len(servers))
	for i, server := range servers {
		upstreamConfig[i] = fmt.Sprintf("server %s:%d weight=%d;", server.Server, server.Port, server.Weight)
	}

	// Construct the complete Nginx configuration
	nginxConf := fmt.Sprintf(`
	upstream backend {
		%s
	}
	log_format custom '$remote_addr - $remote_user [$time_local] "$request" '
                  '$status $body_bytes_sent "$http_referer" '
                  '"$http_user_agent" "$http_x_forwarded_for" '
                  'upstream=$upstream_addr '
                  'upstream_response_time=$upstream_response_time';


	access_log /var/log/nginx/access.log custom;

	server {
		listen 80;

		location / {
			proxy_pass http://backend;
		}
	}
	`, strings.Join(upstreamConfig, "\n"))

	// Modify the configuration
	configMap.Data["nginx.conf"] = nginxConf

	// Update the ConfigMap
	_, err = clientset.CoreV1().ConfigMaps(namespace).Update(context.TODO(), configMap, metav1.UpdateOptions{})
	if err != nil {
		panic(err)
	}

	fmt.Println("Successfully updated Nginx ConfigMap!")
}

func readLogs(upstreamAddr string) (float64, error) {

	serviceData, err := getServiceIPAndPort("nginx-service")
	if err != nil {
		return 0, fmt.Errorf("Error getting service IP and port: %w", err)
	}

	url := fmt.Sprintf("http://%s:7777/access.log", serviceData.ServiceIP)

	resp, err := http.Get(url)
	if err != nil {
		return 0, fmt.Errorf("error sending request: %w", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)

	var allLines []string
	for scanner.Scan() {
		allLines = append(allLines, scanner.Text())
	}

	if scanner.Err() != nil {
		return 0, fmt.Errorf("error reading response: %w", scanner.Err())
	}

	// Regular expression to match and capture upstream address and response time
	r := regexp.MustCompile(`upstream=(?P<Upstream>\S+) upstream_response_time=(?P<ResponseTime>\S+)`)

	var totalResponseTime float64
	var count int

	for i := len(allLines) - 1; i >= 0 && count < 100; i-- {
		line := allLines[i]
		matches := r.FindStringSubmatch(line)

		if len(matches) > 0 && matches[1] == upstreamAddr {
			responseTimeString := matches[2]
			responseTime, err := strconv.ParseFloat(responseTimeString, 64)
			if err != nil {
				return 0, fmt.Errorf("error converting response time '%s' to float64: %w", responseTimeString, err)
			}
			totalResponseTime += responseTime
			count++
		}
	}

	if count == 0 { // Prevent division by zero
		return 0, nil
	}

	averageResponseTime := totalResponseTime / float64(count)
	return averageResponseTime, nil
}
