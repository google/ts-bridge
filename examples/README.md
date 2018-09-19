# Alerting Policy

Time Series Bridge exports a Stackdriver metric `oldest_metric_age` that can be
used to detect configured metrics that no longer get updated.

`policy.yaml` file in this directory contains a sample Stackdriver alerting
policy called "Time Series Bridge Stalled". It will trigger if at least one of
the metrics have not had any data imported for 30 minutes.

The policy also has a metric absense condition that will trigger if the
`oldest_metric_age` metric itself has not been updated for 30 minutes. This
can be used to detect issues with Time Series Bridge itself.

You can push this sample policy to your Stackdriver project by running:

```
gcloud alpha monitoring policies create --policy-from-file=policy.yaml
```

Note, that the policy does not actually have any notifications enabled, so
you will need to adjust it manually (or create another YAML file based on
`policy.yaml`) if you'd like to be notified when it triggers.