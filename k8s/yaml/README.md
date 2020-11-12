# K8s example

This provides an extremely basic "bare-bones" GKE deployment example for K8s using:
 - BoltDB and PD-SSD for persistence
 - Basic `ClusterIP` service for exposing the web ui

## Quickstart

1) Edit the configuration in `config_map.yaml`

2) Add the service account key secret in secret.yaml by parsing an account key using `base64`, e.g.:

    ```
    $ base64 ~/.gcp/my-account-key.json
    ```
   
   Note: secret is not required if you're using [GKE Workload Identity](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity)
   
3) Specify an image source in `deployment.yaml`

4) Apply the files to your cluster:

    ```
    $ kubectl apply -f ./k8s/yaml/
    ```

## TODO:

- Add RBAC example
- Add SecurityPolicy example
- Add GKE WorkloadIdentity example