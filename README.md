# ReplicaBalancer

## Description
ReplicaBalancer is a custom Kubernetes scaling engine designed to enhance system reliability and performance. It dynamically adjusts pod replicas based on real-time system metrics, optimizing resource allocation and system stability. This engine is equipped to handle various operational challenges, including pod failures, memory leaks, and response time variability, making it ideal for cloud-native applications that require robust and adaptive scaling strategies.

You can find the paper with details of the proposed framework here: https://github.com/nazanin97/ReplicaBalancer/blob/0122f8ca67b15fe497d3329dd1492dabe9ce9577/Cloud_Native_Applications_with_Diverse_Microservices.pdf

## Prerequisites
To use ReplicaBalancer, ensure your environment meets the following requirements:
- A Kubernetes cluster
- Prometheus installed (for metrics gathering)
- Grafana installed (for metrics visualization)
- Chaos Mesh installed (for conducting chaos experiments)

## Installation
### Persistent Volume Claim (PVC) Setup
Before deploying the ReplicaBalancer, set up a Persistent Volume Claim (PVC) to provide persistent storage for the application. This is crucial for retaining data across pod restarts and deployments. Apply the `my-pvc.yaml` configuration using the following command:

```bash
kubectl apply -f my-pvc.yaml
```

The `my-pvc.yaml` file should contain the following configuration:

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: my-pvc
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
```

### ReplicaBalancer Deployment
To install and deploy the ReplicaBalancer in your Kubernetes cluster, follow these steps:

1. **Prepare the Deployment YAML File:**
   Below is the content of the `$ kubectl apply -f AppDeploymentFile.yaml` file, which defines the Kubernetes Deployment for the ReplicaBalancer.

   ```yaml
   apiVersion: apps/v1
   kind: Deployment
   metadata:
     name: ReplicaBalancer
   spec:
     replicas: 1
     selector:
       matchLabels:
         app: ReplicaBalancer-app
     template:
       metadata:
         labels:
           app: ReplicaBalancer-app
       spec:
         containers:
         - name: ReplicaBalancer-app
           image: nazaninakhtarian/replica-balancer:latest
           imagePullPolicy: Always
           volumeMounts:
           - mountPath: "/var/data"
             name: my-pvc
         volumes:
         - name: my-pvc
           persistentVolumeClaim:
             claimName: my-pvc
   ```

2. **Deploy the Application:**
   Use the following `kubectl` command to apply the deployment defined in the YAML file:

   ```bash
   kubectl apply -f AppDeploymentFile.yaml
   ```

   This command will create the necessary deployment based on the specifications in the `AppDeploymentFile.yaml` file. After executing these steps, verify that the deployment is running successfully in your Kubernetes cluster.

### NGINX Deployment and Configuration
To support the ReplicaBalancer, NGINX is used as a reverse proxy and load balancer. Follow these steps to deploy and configure NGINX:

1. **Deploy NGINX:**
Create a deployment for NGINX using the `nginx-deployment.yaml` file. This configuration sets up the NGINX server and a log sidecar container.

#### Deployment Command

```bash
kubectl apply -f nginx-deployment.yaml
```

#### YAML File: `nginx-deployment.yaml`

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx-deployment
spec:
  replicas: 1
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      - name: nginx
        image: nginx:latest
        ports:
        - containerPort: 80
        volumeMounts:
        - name: config-volume
          mountPath: /etc/nginx/conf.d/
        - name: nginx-logs
          mountPath: /var/log/nginx/
      - name: log-sidecar
        image: busybox
        command: 
          - "/bin/sh"
        args:
          - "-c"
          - "busybox httpd -f -p 7777 -h /var/log/nginx/"
        ports:
        - containerPort: 7777
        volumeMounts:
        - name: nginx-logs
          mountPath: /var/log/nginx/
      volumes:
      - name: config-volume
        configMap:
          name: nginx-conf
      - name: nginx-logs
        emptyDir: {}
```

2. **Set Up NGINX Service:**
Set up the NGINX service to make NGINX accessible within your Kubernetes cluster using the `nginx-service.yaml` file.

#### Command

```bash
kubectl apply -f nginx-service.yaml
```

#### YAML File: `nginx-service.yaml`

```yaml
apiVersion: v1
kind: Service
metadata:
  name: nginx-service
spec:
  type: LoadBalancer
  selector:
    app: nginx
  ports:
  - name: web
    port: 3333
    targetPort: 80
  - name: logs
    port: 7777
    targetPort: 7777
```

4. **Configure NGINX:**
The `nginx-config.yaml` file contains NGINX configuration, including custom logging and upstream settings. This is applied to create a ConfigMap in Kubernetes.

#### Command

```bash
kubectl apply -f nginx-config.yaml
```

#### YAML File: `nginx-config.yaml`

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: nginx-conf
data:
  nginx.conf: |
    log_format custom '$remote_addr - $remote_user [$time_local] "$request" '
                      '$status $body_bytes_sent "$http_referer" '
                      '"$http_user_agent" "$http_x_forwarded_for" '
                      'upstream=$upstream_addr '
                      'upstream_response_time=$upstream_response_time';

    access_log /var/log/nginx/access.log custom;

    upstream backend {
      server frontend-faulty-deployment-service.default.svc.cluster.local:8010 weight=1;
      server frontend-inconsistent-response-deployment-service.default.svc.cluster.local:8011 weight=1;
      server frontend-memory-leak-deployment-service.default.svc.cluster.local:8012 weight=1;
    }

    server {
        listen 80;
        location / {
            proxy_pass http://backend;
        }
    }
```

## Configuration
Configure the ReplicaBalancer to suit your specific requirements by setting the necessary environment variables using the `kubectl set env` command. Among these variables, `DEPLOYMENT_IMAGES_REPLICAS` is mandatory, as it defines the deployment images. The other variables are optional; if you do not specify them, the system will use the following default values:

```bash
kubectl set env deployment/ReplicaBalancer \
DEPLOYMENT_IMAGES_REPLICAS="imageName1*replica1,imageName2*replica2,imageName3*replica3" \
TOTAL_REPLICAS=9 \
MONITORING_TIME=30s \
ACTION_TIME=2m \
MAX_REPLICAS=24 \
MIN_REPLICAS=3 \
MAX_CPU=60 \
MIN_CPU=20 \
SCALING=false
```

## Testing and Validation
After configuring the ReplicaBalancer, it's crucial to test and validate its performance to ensure that it meets your system's requirements. Follow these steps to effectively test and validate the scaling engine:

### Testing Steps:

1. **Initial Deployment Verification:**
   - Verify that the ReplicaBalancer is correctly deployed and running in your Kubernetes environment.
   - Check if all pods are in a 'running' state using the command: `kubectl get pods`.

2. **Load Testing:**
   - Simulate a typical workload on your system using tools like Locust for generating realistic traffic patterns. Ensure that the load generator targets the IP address and port where the NGINX server is listening. This ensures that traffic is correctly routed through the NGINX load balancer to your application.
   - Observe how the ReplicaBalancer responds to changes in workload. Look for changes in the number of pod replicas, and monitor the system's ability to scale dynamically.

3. **Chaos Testing:**
   - Introduce various failure scenarios using Chaos Mesh to test the resilience of the ReplicaBalancer.
   - Monitor how the system recovers and scales in response to these induced failures.
