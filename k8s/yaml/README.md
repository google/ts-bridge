# K8s example

This provides an extremely basic "bare-bones" GKE deployment example for K8s using:
 - BoltDB and PD-SSD for persistence
 - Basic `ClusterIP` service for exposing the web ui

## Quickstart

For convenience, `ts-bridge` provides 2 example deployments:

- Using GKE workload identity, avoiding service account key management. (recommended)
- Using standard Kubernetes `Secret`, which can be adapted further for different secret storage mechanisms.

### GKE Workload Identity (recommended)

#### Setting up workload identity

1) This tutorial assumes that you have:

   - [Enabled GKE workload identity](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity#enable_on_cluster)
   - Created a dedicated namespace for the app, in this example it will be `ts-bridge`

1) Create a service account for ts-bridge:

    ```
    $ gcloud iam service-accounts create ts-bridge-sa \
        --description="TSBridge service account"
    
    ```

1) Assign it the `Monitoring.Editor` role:

    ```
    $ gcloud projects add-iam-policy-binding PROJECT \
        --member="serviceAccount:ts-bridge-sa@PROJECT.iam.gserviceaccount.com" \
        --role="roles/monitoring.editor"
    ```
    , where `PROJECT` is your cloud project id.

1)  Allow GKE service account to impersonate this service account:

    ```
    $ gcloud iam service-accounts add-iam-policy-binding \
     --role roles/iam.workloadIdentityUser \
     --member "serviceAccount:PROJECT.svc.id.goog[ts-bridge/GKE_SERVICE_ACCOUNT_NAME]" \
     ts-bridge-sa@PROJECT.iam.gserviceaccount.com
    ```
    , where `GKE_SERVICE_ACCOUNT_NAME` is your GKE service account name.
    Note: if you're using the default service account you can just specify `default` instead of `GKE_SERVICE_ACCOUNT_NAME`

1) Annotate the GKE Service Account:

    ```
    $ kubectl annotate serviceaccount \
      --namespace ts-bridge \
      GKE_SERVICE_ACCOUNT_NAME \
      iam.gke.io/gcp-service-account=ts-bridge-sa@PROJECT.iam.gserviceaccount.com
    ```

1) You can test whether it worked via running a `gcloud` test pod and issuing `gcloud auth list`, e.g.:

```
    $ kubectl run -it \
    --image google/cloud-sdk:slim \
    --serviceaccount default \
    --namespace ts-bridge \
      workload-identity-test

    root@workload-identity-test:/# gcloud auth list
                        Credentialed Accounts
    ACTIVE  ACCOUNT
    *       ts-bridge-sa@my-project.iam.gserviceaccount.com
```

1) Then edit the metrics configuration in `config_map.yaml`

1) Specify an image source in `deployment.yaml`

1) Apply the files to your cluster:

    ```
    $ kubectl apply -f ./k8s/yaml/workload-identity-example
    ```


### Passing credentials as a secret

1) Edit the metrics configuration in `config_map.yaml`

2) Add the service account key secret in secret.yaml by parsing an account key using `base64`, e.g.:

    ```
    $ base64 ~/.gcp/my-account-key.json
    ```
     
3) Specify an image source in `deployment.yaml`

4) Apply the files to your cluster:

    ```
    $ kubectl apply -f ./k8s/yaml/secret-example
    ```

## TODO:

- Add RBAC example
- Add SecurityPolicy example
