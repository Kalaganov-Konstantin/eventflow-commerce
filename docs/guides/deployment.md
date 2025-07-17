# Deployment Guide

This guide explains how to deploy the EventFlow Commerce application to a Kubernetes cluster.

## Prerequisites
- A running Kubernetes cluster
- `kubectl` configured to access the cluster
- Helm (optional)

## Deployment Steps
1. Configure your secrets in `infrastructure/k8s/secrets.yaml`.
2. Apply the Kubernetes manifests:
   ```bash
   kubectl apply -f infrastructure/k8s/
   ```
3. Verify that all pods are running.
