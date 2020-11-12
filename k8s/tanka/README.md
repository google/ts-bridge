# TS-Bridge sample k8s deployment

This is a very simplistic example of a deployment of TS-Bridge done with [Tanka](https://tanka.dev/).

## Quickstart

Install tanka: https://tanka.dev/install

1) Configure your API endpoint, e.g.:
    
    ```
    $ cd ./k8s/tanka
    $ tk env set environments/default --server=https://127.0.0.1:6443
    ```

2) (Optional) Set a namespace you would like to use:

    ```
    $ tk env set environments/default --namespace my-namespace
    ```

3) You can set the endpoint from your current context by issuing this command:

    ```
    $ SERVER_ENDPOINT=$(kubectl config view -o jsonpath='{"Cluster name\tServer\n"}{range .clusters[*]}{.name}{"\t"}{.cluster.server}{"\n"}{end}' | grep `kubectl config current-context` | cut -f2)
    $ tk env set environments/default --server=${SERVER_ENDPOINT}
    ```

4) Configure the deployment:

    Edit the `environments/default/main.jsonnet.example` to specify running parameters and move it to live config:
    
    ```
    $ mv environments/default/main.jsonnet.example environments/default/main.jsonnet
    ```
   
    Then, configure metrics in `environments/default/config/metrics_config.jsonnet` and move it to live config:
    
    ```
    $ mv environments/default/config/metrics_config.jsonnet environments/default/config/metrics_config.jsonnet
    ```

    Note: alternatively, you can convert an existing YAML config with [yq](https://github.com/mikefarah/yq) utility, e.g.:
    
    ```
    yq r ../../metrics.yaml -j -P > environments/default/config/metrics.jsonnet
    ``` 

5) Apply the configuration to your cluster:

    ```
    $ tk apply environments/default/
    ```
   
## TODO:

- Add RBAC example
- Add SecurityPolicy example
- Add GKE WorkloadIdentity example