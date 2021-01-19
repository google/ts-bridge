"""Script to parse results from Trivy scan.

1) Reads in results from two files (json and table format) generated by Trivy.
2) Creates a GitHub issue in the given repo if any vulnerabilities were found.
3) Fails the CI build if any vulerabilities of HIGH/CRITICAL severity exist.

Both the json and table Trivy results must be created prior to running
this script. These can be generated with:
    trivy image --format json --light --no-progress -o <trivy_output>.json
        gcr.io/cre-tools/ts-bridge
    trivy image --light  --no-progress -o <trivy_output>.table
        gcr.io/cre-tools/ts-bridge
"""
import json
import sys
import textwrap
from absl import app
from absl import flags
from github import BadAttributeException
from github import Github
from github import GithubException
from pathlib import Path

FLAGS = flags.FLAGS
flags.DEFINE_string(
    "build_id", None,
    "ID of the current Cloud Build, specified using $BUILD_ID substitution")
flags.DEFINE_string(
    "commit_id", None,
    "ID of the current commit, specified using $COMMIT_ID substitution")
flags.DEFINE_string(
    "release_tag", None, "Number of the latest release/tag. "
    "This can be retrieved using \n "
    "git describe --abbrev=0 --tags > _release_tag")
flags.DEFINE_string(
    "repo_name", None,
    "Name of the github repo where potential issues will be created.")
flags.DEFINE_string(
    "token_file", None,
    "Name of the file where the GitHub token has been stored.\n"
    "This can be created using \n"
    "gcloud secrets versions access latest "
    "--secret=Ts-bridge-bot-token --format='get(payload.data)' | "
    "tr '_-' '/+' | base64 -d > git_token.txt")
flags.DEFINE_string("trivy_file", None,
                    "Name of the files where results from Trivy are stored")

flags.mark_flag_as_required("build_id")
flags.mark_flag_as_required("commit_id")
flags.mark_flag_as_required("release_tag")
flags.mark_flag_as_required("repo_name")
flags.mark_flag_as_required("token_file")
flags.mark_flag_as_required("trivy_file")


def validate_flags():
    error = None
    if not Path("{}.json".format(FLAGS.trivy_file)).is_file():
        error = ("Please run \n"
                 "trivy image --format json --light --no-progress -o {}.json "
                 "gcr.io/cre-tools/ts-bridge \n").format(FLAGS.trivy_file)
    elif not Path("{}.table".format(FLAGS.trivy_file)).is_file():
        error = ("Please run \n"
                 "trivy image --format --light --no-progress -o {}.table "
                 "gcr.io/cre-tools/ts-bridge \n").format(FLAGS.trivy_file)
    elif not Path(FLAGS.token_file).is_file():
        error = ("Please run \n"
                 "gcloud secrets versions access latest "
                 "--secret=Ts-bridge-bot-token --format='get(payload.data)' | "
                 "tr '_-' '/+' | base64 -d > {}").format(FLAGS.token_file)
    if error:
        sys.exit(error)


def load_results():
    """ Load the results from Trivy."""
    trivy_out_json = "{}.json".format(FLAGS.trivy_file)
    trivy_out_table = "{}.table".format(FLAGS.trivy_file)
    with open(trivy_out_json) as f:
        # trivy_result is the json result used for parsing vulnerabilities
        # Index is 0 because there is only one target built.
        trivy_result = json.load(f)[0]
    with open(trivy_out_table) as f:
        # trivy_table is the results in a more readable table format which can
        # be included in GitHub issues.
        trivy_table = f.read()
    return [trivy_result, trivy_table]


def get_severity_list(vulnerabilities):
    """Filters out all the severities in a given list of vulnerabilities."""
    severity_list = [v.get("Severity") for v in vulnerabilities]
    return sorted(set(severity_list))


def high_or_critical_exists(severity_list):
    return "HIGH" in severity_list or "CRITICAL" in severity_list


def get_github_repo():
    """Connects to GitHub API and returns a repo object."""
    with open(FLAGS.token_file) as f:
        token = f.read()
    github = Github(token)
    try:
        repo = github.get_repo(FLAGS.repo_name)
    except (BadAttributeException, GithubException) as e:
        sys.exit(
            ("Failed to get repo with name {} due to an exception from GitHub."
             "\nThe error returned by GitHub API was {}").format(
                FLAGS.repo_name, e))

    return repo


def create_issue(target_name, num_vulnerabilities, severity_list, table):
    """Creates a Github issue with vulnerabilities as description."""
    repo = get_github_repo()

    title = ("Vulnerability [{}] found in release {}").format(
        ",".join(severity_list), FLAGS.release_tag)

    body = ("Trivy has detected {} vulnerabilities in your latest "
            "build.").format(num_vulnerabilities)

    if high_or_critical_exists(severity_list):
        title += ": Images from commit {} cannot be released".format(
            FLAGS.commit_id)

        body += " Please correct this issue so the new images can be "
        "published on Container Registry."

    body_args = dict(build_id=FLAGS.build_id, commit_id=FLAGS.commit_id,
                     release_tag=FLAGS.release_tag, target_name=target_name, table=table)
    body += textwrap.dedent("""
        \n
        **Cloud Build ID:** {build_id}
        **Commit ID:** {commit_id}
        **Tag:** {release_tag}
        **Target:** {target_name}
        ```
        {table}
        ```""").format(**body_args)
    print(body)

    try:
        new_issue = repo.create_issue(title=title, body=body)
    except (BadAttributeException, GithubException) as e:
        sys.exit(
            ("Failed to create issue due to an exception from GitHub.\nThe "
             "error returned by GitHub API was {}").format(e))

    return new_issue.number


def main(argv):
    validate_flags()
    [trivy_result, trivy_table] = load_results()

    # Examine results to check if vulnerabilities were found
    target = trivy_result["Target"]
    vulnerabilities = trivy_result["Vulnerabilities"]
    if vulnerabilities:
        num_vulnerabilities = len(vulnerabilities)
        severity_list = get_severity_list(vulnerabilities)
        issue_number = create_issue(target, num_vulnerabilities, severity_list,
                                    trivy_table)

        details = ("{} vulnerabilities of type: [{}] were found in image. "
                   "Please refer to issue: {} for details.").format(
                       num_vulnerabilities, ",".join(severity_list), issue_number)
        print(details)

        if high_or_critical_exists(severity_list):
            sys.exit("Build will be aborted and "
                     "new images will not be pushed to GCR.")
        else:
            print("Images will be published to GCR.")
    else:
        print("No vulnerabilities found. Images will be published to GCR.")


if __name__ == "__main__":
    app.run(main)