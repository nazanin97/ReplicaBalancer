package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
)

func resetMetricsData(deploymentName string) {
	filePath := "/var/data/metrics.json"
	data := make(map[string]map[string][]float64)

	// Read existing data from file, if any
	jsonData, err := ioutil.ReadFile(filePath)
	if err == nil {
		// Unmarshal the existing data into the map
		err = json.Unmarshal(jsonData, &data)
		if err != nil {
			fmt.Printf("Failed to parse existing data: %v\n", err)
			return
		}
	}

	// Delete the metrics data of the given deployment
	fmt.Println("Reset json file...")
	delete(data, deploymentName)
	// Convert the map to JSON
	jsonData, err = json.MarshalIndent(data, "", "    ")
	if err != nil {
		fmt.Println(err)
		return
	}

	// Write the JSON data to a file
	err = ioutil.WriteFile(filePath, jsonData, 0644)
	if err != nil {
		fmt.Println(err)
	}
}

func writeMetricsData(deploymentName string, restarts float64, averageResponseTime float64, memoryUsage float64) {
	filePath := "/var/data/metrics.json"
	data := make(map[string]map[string][]float64)

	// Read existing data from file, if any
	jsonData, err := ioutil.ReadFile(filePath)
	if err == nil {
		// Unmarshal the existing data into the map
		err = json.Unmarshal(jsonData, &data)
		if err != nil {
			fmt.Printf("Failed to parse existing data: %v\n", err)
			return
		}
	}

	// Initialize the inner map if it does not exist
	if _, exists := data[deploymentName]; !exists {
		data[deploymentName] = make(map[string][]float64)
	}

	// Append the values to the respective slices
	data[deploymentName]["restarts"] = append(data[deploymentName]["restarts"], restarts)
	data[deploymentName]["memoryUsage"] = append(data[deploymentName]["memoryUsage"], memoryUsage)
	data[deploymentName]["averageResponseTime"] = append(data[deploymentName]["averageResponseTime"], averageResponseTime)

	// Convert the map to JSON
	jsonData, err = json.MarshalIndent(data, "", "    ")
	if err != nil {
		fmt.Println(err)
		return
	}

	// Write the JSON data to a file
	err = ioutil.WriteFile(filePath, jsonData, 0644)
	if err != nil {
		fmt.Println(err)
	}
}

func readMetricsData(deploymentName string) map[string][]float64 {
	// Read the file
	jsonData, err := ioutil.ReadFile("/var/data/metrics.json")
	if err != nil {
		fmt.Println(err)
		return nil
	}

	// Define a variable to hold our decoded data
	var data map[string]map[string][]float64

	// Unmarshal the JSON data into our map
	err = json.Unmarshal(jsonData, &data)
	if err != nil {
		fmt.Println(err)
		return nil
	}

	// Check if the deployment exists in the map
	if deploymentData, ok := data[deploymentName]; ok {
		return deploymentData
	} else {
		fmt.Errorf("no data for deployment %s", deploymentName)
		return nil
	}
}
func readAllMetricsData() map[string]map[string][]float64 {
	// Read the file
	jsonData, err := ioutil.ReadFile("/var/data/metrics.json")
	if err != nil {
		fmt.Println(err)
		return nil
	}

	// Define a variable to hold our decoded data
	var data map[string]map[string][]float64

	// Unmarshal the JSON data into our map
	err = json.Unmarshal(jsonData, &data)
	if err != nil {
		fmt.Println(err)
		return nil
	}

	return data
}
