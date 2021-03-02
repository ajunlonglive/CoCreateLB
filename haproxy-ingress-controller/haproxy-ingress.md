
# Deploy HAProxy as an Ingress Controller LoadBalancing L4 Traffic to per-Node Nginx Ingress Controller from Rancher

To better integrate with fleet CI, this doc uses `helm template` to generate manifests for deployment.

## Install helm

Refer to [helm](https://helm.sh/).

## Add official helm repo of haproxy

```bash
 helm repo add haproxytech https://haproxytech.github.io/helm-charts
 helm repo update
```

## Customize configuration of helm by `values.yaml`

See comments below for some detailed information.

```yaml
# values.yaml
rbac:
  create: true

controller:
  name: controller
  image:
    repository: haproxytech/kubernetes-ingress
    tag: "{{ .Chart.AppVersion }}"
    pullPolicy: IfNotPresent

  # use DaemonSet so no need to manage replicas
  kind: DaemonSet

  terminationGracePeriodSeconds: 300

  # if use private registry, set values below
  imageCredentials:
    registry: null
    username: null
    password: null
  existingImagePullSecret: null

  # the http and https frontends are disabled later
  # here just keep the ports 80 and 443 for reusing
  # in L4 proxy
  containerPort:
    http: 80
    https: 443
    # for exposing prometheus-format metrics
    stat: 1024

  livenessProbe:
    failureThreshold: 3
    initialDelaySeconds: 0
    path: /healthz
    periodSeconds: 10
    port: 1042
    scheme: HTTP
    successThreshold: 1
    timeoutSeconds: 1

  readinessProbe:
    failureThreshold: 3
    initialDelaySeconds: 0
    path: /healthz
    periodSeconds: 10
    port: 1042
    scheme: HTTP
    successThreshold: 1
    timeoutSeconds: 1

  startupProbe:
    failureThreshold: 20
    initialDelaySeconds: 0
    path: /healthz
    periodSeconds: 1
    port: 1042
    scheme: HTTP
    successThreshold: 1
    timeoutSeconds: 1

  # ingressClass can be used to distingush which ingresses
  # the haproxy ingress controller should care
  # see https://kubernetes.io/docs/concepts/services-networking/ingress/#ingress-class
  # note that this only affects L7 proxy as L4 proxy
  # doesn't rely on ingress and ingressClass
  ingressClass: haproxy

  # in L4 proxy, tls termination is no need
  defaultTLSSecret:
    enabled: false
    secret: null

  # increase below values for more resource capacity
  resources:
    # limits:
        # cpu: 100m
        # memory: 64Mi
    requests:
      cpu: 100m
      memory: 64Mi

  # DaemonSet will only deploy pods on nodes
  # with the label "nodeType=loadbalancer"
  # make sure the per-node nginx ingress controller
  # from Rancher is not deployed on those nodes
  # see https://rancher.com/docs/rke/latest/en/config-options/add-ons/ingress-controllers/#scheduling-ingress-controllers for how to select nodes for ingress in rke
  nodeSelector:
    nodeType: loadbalancer
  
  # this is for using host network in haproxy pods
  # if loadbalancer type service is used to expose haproxy pods
  # set below to ClusterFirst   
  dnsPolicy: ClusterFirstWithHostNet
  
  
  extraArgs:
    # define L4 proxy servic via port mapping in ConfigMap entries
    # example: "80": ingress-nginx/ingress-nginx:80
    # this directs traffic arrives at 80 port of haproxy
    # to the port of service "ingress-nginx"
    # in the namespace "ingress-nginx"
    - --configmap-tcp-services=ingress-haproxy/tcp-services
    # disable default http frontend on 80 port
    # so 80 port can be used for L4 proxy
    - --disable-http
    # disable default https frontend on 443 port
    # so 443 port can used for L4 proxy
    - --disable-https

  logging:
    level: info
    traffic:
      address: stdout
      # format: raw
      facility: daemon

  # if host network is used, i.e. haproxy runs
  # in host's network namespace and occupies host's ports,
  # service is no need
  # set "enabled" to "true" if use loadbalancer type service to expose haproxy
  service:
    enabled: false
    type: LoadBalancer

    ports:
      http: 80
      https: 443
      stat: 1024

    enablePorts:
      http: true
      https: true
      stat: true

    targetPorts:
      http: http
      https: https
      stat: stat
    
    # no need to set extra tcp ports
    # as port 80 and 443 are reused  
    tcpPorts: []

    # externalIPs:
    # - xxx.xxx.xxx.xxx

  # use host network in haproxy pod
  daemonset:
    useHostNetwork: true
    useHostPort: false

  priorityClassName: system-cluster-critical

serviceMonitor:
    enabled: false

    extraLabels: {}

    endpoints:
    - port: stat
      path: /metrics
      scheme: http

# only needed if http and https frontends are enabled
defaultBackend:
  enabled: false
  name: default-backend
  replicaCount: 1

  image:
    repository: k8s.gcr.io/defaultbackend-amd64
    tag: 1.5
    pullPolicy: IfNotPresent
    runAsUser: 65534

  containerPort: 8080

  nodeSelector:
    nodeType: loadbalancer

  service:
    port: 8080

  serviceAccount:
    create: true
```

## Generate manifests for haproxy ingress controller

```bash
helm template haproxy-ingress haproxytech/kubernetes-ingress -n ingress-haproxy -f ./values.yaml > haproxy-ingress.yaml
```

## Define namespace runs haproxy ingress controller

```bash
cat <<EOF > ns-ingress-haproxy.yaml
---
apiVersion: v1
kind: Namespace
metadata:
  name: ingress-haproxy
spec: {}
status: {}
---
EOF
```

## Define configmap describing services handled by L4 proxy

```bash
cat <<EOF > cm-tcp-services.yaml
---
apiVersion: v1
data:
  "80": ingress-nginx/ingress-nginx:80
  "443": ingress-nginx/ingress-nginx:443
kind: ConfigMap
metadata:
  creationTimestamp: null
  name: tcp-services
  namespace: ingress-haproxy
---
EOF
```

## Define service targeting at all available nginx ingress controller instances

```bash
cat <<EOF > ing-ingress-nginx.yaml
---
apiVersion: v1
kind: Service
metadata:
  creationTimestamp: null
  labels:
    app: ingress-nginx
  name: ingress-nginx
  namespace: ingress-nginx
spec:
  ports:
  - name: http
    port: 80
    protocol: TCP
    targetPort: 80
  - name: https
    port: 443
    protocol: TCP
    targetPort: 443
  selector:
    # ensure the right label is used
    app: ingress-nginx
  type: ClusterIP
status:
  loadBalancer: {}
---
EOF
```

## Put it together

```bash
# combine files together in order
cat ns-ingress-haproxy.yaml > haproxy-ingress-manifests.yaml
cat cm-tcp-services.yaml >> haproxy-ingress-manifests.yaml
cat ing-ingress-nginx.yaml >> haproxy-ingress-manifests.yaml
cat haproxy-ingress.yaml >> haproxy-ingress-manifests.yaml

# apply
kubectl apply -f haproxy-ingress-manifests.yaml
```
