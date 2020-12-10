# Used by cloudbuild.yaml to tag images with correct versions based on git version tags.
# This script assumes that there 
try:
    with open("_FULL_TAG") as f:
        version = f.readline()
except FileNotFoundError:
    print("Please run git describe --abbrev=0 --tags > _FULL_TAG first.")
    quit()

version = version.strip().split(".")

with open("_MAJOR_TAG", "w") as f:
    f.write("{0}".format(version[0]))

with open("_MINOR_TAG", "w") as f:
    f.write("{0}.{1}".format(version[0], version[1]))