# extended-deployment

Extended Deployment, supporting multi-architecture deployment, partition deployment, fault detection, and automatic scheduling scenarios

## Features
- Partition Deployment: Deploy a service simultaneously across multiple partitions.
- Partition: A partition is a logical concept, a logical grouping of Nodes that meet certain conditions, typically achieved by setting labels on Nodes and then setting application node affinity. For example, label all ARM architecture Nodes in the cluster with: kubernetes.io/arch: arm64, and all AMD architecture Nodes with: kubernetes.io/arch: amd64. These two labels can divide the cluster into two partitions, ARM and AMD.
- Partition Reload: There may be differences between partitions, and there may also be configuration differences when deploying applications (such as image architecture differences). Partition reload achieves differentiated deployment between partitions.
- Group Rolling: During release and upgrade, the process of rolling replicas to the target state is done in groups. The next group will only start rolling after the current group is completed. The number of replicas in each group is determined by step control.
- Step Control: During release and upgrade, the size of the group can be controlled by step control during the replica group rolling process.
Beta/Group Release: Beta release means that when creating a partition, the number of replicas is 1, while Group release means that the number of replicas is the group size when creating a partition. The purpose of Beta release is to verify whether the business configuration is correct at the beginning of the release. If there is an error, it can be corrected in time to avoid a large number of unavailable Pods due to configuration errors, consuming resources.
- Wait for Confirmation: By setting a confirmation mechanism, during the release and upgrade process, each group needs manual confirmation before the next group starts.
- Automatic Scheduling for Resource Shortage: During partition deployment, if a partition is short of resources and Pods cannot be scheduled, these replicas can be automatically transferred to other resource-sufficient partitions under the ExtendedDeployment.
- Partition Fault Detection and Automatic Scheduling: Timely detection of whether partition nodes have faults. Once all nodes in a partition are detected to be faulty, partition fault scheduling will be triggered, and replicas in that partition will be scheduled to other normal partitions.
- In-place Upgrade: If only the image is modified during the upgrade, an in-place upgrade can be performed, retaining the Pod and only replacing the container within the Pod.

## Concept
To achieve the above functionalities, three types of resources are designed: DeployRegion, ExtendedDeployment, and InplaceSet.

### DeployRegion
DeployRegion is a partition resource, belonging to the global cluster scope, existing solely as a partition definition. It is created during the cluster environment preparation phase and cannot be created, modified, or deleted once the environment is ready.
> Note:
> 1. When creating a partition, ensure that a node does not belong to multiple partitions simultaneously.
> 2. It is advisable to set corresponding partition labels for all nodes to avoid certain node resources being unusable during partition deployment.
> 3. In principle, partitions should not be modified once the cluster environment is ready. Modifications may lead to incorrect states in ExtendedDeployment that reference the partition, causing applications to malfunction.
> 
> If partition modification is necessary, the following conditions must be met first:
> 1. When adding a partition, ensure that no node belongs to multiple partitions after the addition.
> 2. When modifying a partition, only the selection labels of the partition can be changed. Before modification, ensure that no ExtendedDeployment business associated with the partition exists, as this may cause discrepancies between the actual deployment nodes of the ExtendedDeployment and the expected nodes after modification. After modification, ensure that no node belongs to multiple partitions.
> 3. When deleting a partition, ensure that no ExtendedDeployment is associated with the partition, as this will cause subsequent state synchronization failures for the ExtendedDeployment associated with the partition.

### ExtendedDeployment
ExtendedDeployment is an application deployment resource, similar to Deployment in k8s. It mainly implements partition control, release strategies, fault awareness, and automatic transfer functions. An ExtendedDeployment can control multiple InplaceSets.

### InplaceSet
InplaceSet is a partition application resource, similar to ReplicasSet in k8s. An InplaceSet is associated with a DeployRegion, corresponding to a partition, and implements replica maintenance and in-place upgrade functions.

### Compatibility with K8S Workload Resources (Deployment/ReplicaSet)
In addition to InplaceSet, ExtendedDeployment can also control native K8S Deployment and ReplicaSet resources. Compared to InplaceSet, native resources do not have in-place upgrade functionality, but under the control of ExtendedDeployment, they also possess features such as partition deployment, partition reload, group rolling, step control, Beta/Group release, and automatic scheduling. If an application does not require in-place upgrades, it can specify the managed type as Deployment or ReplicaSet.

## Getting Started

### Prerequisites
- go version v1.22.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### To Deploy on the cluster
**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/extended-deployment:tag
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/extended-deployment:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## Project Distribution

Following are the steps to build the installer and distribute this project to users.

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=<some-registry>/extended-deployment:tag
```

NOTE: The makefile target mentioned above generates an 'install.yaml'
file in the dist directory. This file contains all the resources built
with Kustomize, which are necessary to install this project without
its dependencies.

2. Using the installer

Users can just run kubectl apply -f <URL for YAML BUNDLE> to install the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/extended-deployment/<tag or branch>/dist/install.yaml
```

## License

Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

