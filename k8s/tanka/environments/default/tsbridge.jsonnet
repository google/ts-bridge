{
  tsbridge: {
              deployment: {
                apiVersion: 'apps/v1',
                kind: 'Deployment',
                metadata: {
                  name: $._config.tsbridge.name,
                },
                spec: {
                  selector: {
                    matchLabels: {
                      app: $._config.tsbridge.name,
                    },
                  },
                  strategy: {
                    type: 'Recreate',
                  },
                  template: {
                    metadata: {
                      labels: {
                        app: $._config.tsbridge.name,
                      },
                    },
                    spec: {
                      securityContext: {
                        runAsUser: 1000,
                        runAsGroup: 1000,
                        fsGroup: 1000,
                      },
                      containers: [
                        {
                          image: $._config.tsbridge.image,
                          name: $._config.tsbridge.name,
                          env: [
                            {
                              name: 'GOOGLE_APPLICATION_CREDENTIALS',
                              value: '/etc/gcp/google_key.json',
                            },
                          ],
                          args: $._config.tsbridge.args,
                          ports: [
                            {
                              containerPort: 8080,
                              name: $._config.tsbridge.name,
                            },
                          ],
                          resources: {
                            limits: {
                              memory: $._config.tsbridge.memoryLimit,
                            },
                            requests: {
                              memory: $._config.tsbridge.memoryRequest,
                            },
                          },
                          livenessProbe: {
                            httpGet: {
                              path: '/health',
                              port: 8080,
                            },
                            initialDelaySeconds: 3,
                            periodSeconds: 3,
                          },
                          readinessProbe: {
                            httpGet: {
                              path: '/health',
                              port: 8080,
                            },
                            initialDelaySeconds: 3,
                            periodSeconds: 3,
                          },
                          volumeMounts: [
                            {
                              name: 'ts-bridge-config-volume',
                              mountPath: '/ts-bridge/metrics.yaml',
                              subPath: 'metrics.yaml',
                            },
                            {
                              name: 'service-account-credentials-volume',
                              mountPath: '/etc/gcp',
                              readOnly: true,
                            },
                          ] + (if $._config.tsbridge.persistence.enabled then [{
                                 name: 'ts-bridge-persistent-storage',
                                 mountPath: '/ts-bridge',
                               }] else []),
                        },
                      ],
                      volumes: [
                        {
                          name: 'ts-bridge-config-volume',
                          configMap: {
                            name: $._config.tsbridge.name + '-config',
                          },
                        },
                        {
                          name: 'service-account-credentials-volume',
                          secret: {
                            secretName: 'google-api-credentials',
                            items: [
                              {
                                key: 'json_key',
                                path: 'google_key.json',
                              },
                            ],
                          },
                        },
                      ] + (if $._config.tsbridge.persistence.enabled then [{
                             name: 'ts-bridge-persistent-storage',
                             persistentVolumeClaim: {
                               claimName: $._config.tsbridge.name + '-pv-claim',
                             },
                           }] else []),
                    },
                  },
                },
              },
              configMap: {
                apiVersion: 'v1',
                kind: 'ConfigMap',
                metadata: {
                  name: $._config.tsbridge.name + '-config',
                },
                data: {
                  'metrics.yaml': std.manifestYamlDoc(import 'config/metrics.jsonnet'),
                },
              },
              secret: {
                apiVersion: 'v1',
                kind: 'Secret',
                metadata: {
                  name: 'google-api-credentials',
                },
                type: 'Opaque',
                data: {
                  json_key: $._config.tsbridge.auth.serviceAccountJsonSecret,
                },
              },
            } + (if $._config.tsbridge.persistence.enabled then {
                   persistentVolumeClaim: {
                     apiVersion: 'v1',
                     kind: 'PersistentVolumeClaim',
                     metadata: {
                       name: $._config.tsbridge.name + '-pv-claim',
                     },
                     spec: {
                       storageClassName: $._config.tsbridge.name + '-storage-class',
                       accessModes: [
                         'ReadWriteOnce',
                       ],
                       resources: {
                         requests: {
                           storage: $._config.tsbridge.persistence.volumeSize,
                         },
                       },
                     },
                   },
                   storageClass: {
                     apiVersion: 'storage.k8s.io/v1',
                     kind: 'StorageClass',
                     metadata: { name: $._config.tsbridge.name + '-storage-class' },
                     provisioner: $._config.tsbridge.persistence.provisioner,
                     parameters: $._config.tsbridge.persistence.storageClassParameters,
                   },
                 } else {})
            + (if $._config.tsbridge.expose then {
                 service: {
                   apiVersion: 'v1',
                   kind: 'Service',
                   metadata: {
                     name: $._config.tsbridge.name + '-service',
                   },
                   spec: {
                     type: 'ClusterIP',
                     selector: {
                       app: $._config.tsbridge.name,
                     },
                     ports: [
                       {
                         protocol: 'TCP',
                         port: 8080,
                         targetPort: 8080,
                       },
                     ],
                   },
                 },
               } else {}),
}
