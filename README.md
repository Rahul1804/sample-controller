# Kubernetes Controller for Foo Resources

This project demonstrates a Kubernetes controller written in Go that manages custom resources (`Foo` resources) and their associated Deployments in a Kubernetes cluster.

## Overview

The controller watches for changes to `Foo` resources and ensures that corresponding `Deployment` objects are created, updated, or deleted based on the `spec.replicas` field of the `Foo` resource.

### Features:

- Automatically creates or updates a `Deployment` when a `Foo` resource is added or updated.
- Deletes the associated `Deployment` when a `Foo` resource is deleted.
- Uses Kubernetes client-go library for interacting with Kubernetes APIs.
- Utilizes dynamic client and informers for handling custom resources.

## Prerequisites

- Kubernetes cluster (local or remote).
- `kubectl` configured to communicate with your cluster.
- Go programming language (if building or modifying the controller).
- Docker (if containerizing the controller).

## Installation and Setup

1. **Clone the repository:**

   ```bash
   git clone <repository-url>
   cd <repository-name>

2. **Create the CRD**

    ```bash
    kubectl apply -f foo-crd.yaml

3. **Run the program**

    ```bash
    go run main.go --kubeconfig=/Users/rmishra/.kube/config

4. **Create a foo resource to see the magic**

    ```bash
    kubectl apply -f foo.yaml
