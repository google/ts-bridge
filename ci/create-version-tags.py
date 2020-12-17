import sys

# Used by cloudbuild.yaml to tag images with correct versions based on git version tags.
# This script assumes that there exists a file named _release_tag in the container workspace
# already, which contains the latest version number from git.
try:
    with open("_release_tag") as f:
        version = f.read()
except FileNotFoundError:
    print("Please run git describe --abbrev=0 --tags > _release_tag first.")
    sys.exit()

# Remove the 'v' at the start of version tags because we want numerics only
full_version = version.lstrip('v')
split_version = full_version.split(".")

# Overwrite the full tag file so v is not included. E.g. "1.0.0" of v1.0.0
with open("_release_tag", "w") as f:
    f.write("{0}".format(full_version))

# Create a temporary file in container to store the major version. E.g. "1" of v1.0.0
with open("_MAJOR_TAG", "w") as f:
    f.write("{0}".format(split_version[0]))

# Create a temporary file in container to store the minor version. E.g. "1.0" of v1.0
with open("_MINOR_TAG", "w") as f:
    f.write("{0}.{1}".format(split_version[0], split_version[1]))
