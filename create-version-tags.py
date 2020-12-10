# Used by cloudbuild.yaml to tag images with correct versions based on git version tags.
# This script assumes that there exists a file named _FULL_TAG in the container workspace
# already, which contains the latest version number from git.
try:
    with open("_FULL_TAG") as f:
        version = f.readline()
except FileNotFoundError:
    print("Please run git describe --abbrev=0 --tags > _FULL_TAG first.")
    quit()

version = version.strip().split(".")

# Create a temporary file in container to store the major version. E.g. "1" of v1.0.0
with open("_MAJOR_TAG", "w") as f:
    f.write("{0}".format(version[0]))

# Create a temporary file in container to store the minor version. E.g. "1.0" of v1.0
with open("_MINOR_TAG", "w") as f:
    f.write("{0}.{1}".format(version[0], version[1]))