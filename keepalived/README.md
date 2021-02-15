Setup two nodes in Kubernetes cluster which have a label of run-ingress=true. The keepalived.conf comes from a configmap

# Enabling Non-local Binding - Most Distros
On Debian, RHEL & most Linux variants simply add net.ipv4.ip_nonlocal_bind=1 to the end of the /etc/sysctl.conf file and force a reload of the file with the [sudo] sysctl -p command

# Enabling Non-local Binding - RancherOS v0.5.0 and later

Edit the /var/lib/rancher/conf/cloud-config.d/user_config.yml file and add this in an appropriate place:

rancher:
  sysctl:
    net.ipv4.ip_nonlocal_bind: 1
