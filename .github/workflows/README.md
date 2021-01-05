# ts-bridge CI with GitHub Actions

This folder contains GitHub Actions which automate releases, tests and dev builds for ts-bridge.

## Dev Build
To ensure that PRs do not negatively affect docker builds and trivy scans on
existing public images, `test-docker-image-build.yml` performs similar
tasks as the prod builds (please refer to [CI with Cloud Build](https://github.com/google/ts-bridge/blob/master/ci/README.md)), without creating
issues or publishing images to GCR.

This workflow will be triggered by the following conditions:
* On every PR to the master branch
* (Manual) When the "Run Workflow" button is clicked on GitHub Actions tab

The complete flowchart for the dev build is shown below:

![TS-Bridge Dev Build GHA Flowchart](static/ts-bridge-github-actions.png)

Specifically, the GitHub workflow will complete the following:
1. Build a ts-bridge image using the [Dockerfile](https://github.com/google/ts-bridge/blob/master/Dockerfile).
1. Perform security scanning of images using [Trivy](https://github.com/aquasecurity/trivy#docker). This can find image vulnerabilities of low, medium, high or critical severities. If any vulnerabilities were found (of any level of severity), this step will fail.

If any of the above steps fail, the result will show under the "checks"
section of the corresponding GitHub PR.
