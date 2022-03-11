#!/usr/bin/env bash

SECRET_LABEL_KEY="multicluster.odf.openshift.io/secret-type"
SOURCE_LABEL="BLUE"
DESTINATION_LABEL="GREEN"
INTERNAL_LABEL="INTERNAL"
IGNORE_LABEL="IGNORE"
RAMEN_HUB_NAMESPACE="openshift-dr-system"

function clean-hub-cluster {
  echo "Cleaning up hub cluster"
  oc delete mirrorpeers --all
  for NAMESPACE in "$@"; do
    oc delete managedclusteraddon tokenexchange -n "${NAMESPACE}"
    echo "$SECRET_LABEL_KEY in ($SOURCE_LABEL, $DESTINATION_LABEL, $INTERNAL_LABEL, $IGNORE_LABEL)"
    oc delete secrets -l "$SECRET_LABEL_KEY in ($SOURCE_LABEL, $DESTINATION_LABEL, $INTERNAL_LABEL, $IGNORE_LABEL)" -n "${NAMESPACE}"
  done
}


function clean-spoke-cluster {
  echo "Cleaning spoke cluster"

  # Delete all VolumeReplicationClasses
  oc delete vrcs --all

  # Delete all Object Bucket Claims that contain "odrbucket"
  oc get obc -n ${RAMEN_HUB_NAMESPACE} --no-headers=true | awk '/odrbucket/{print $1}'| xargs  oc delete -n ${RAMEN_HUB_NAMESPACE} obc
}

while getopts "h:s:" op; do
    case "${op}" in
        h)
        echo "Cleaning up DR Resources in hub cluster"
        export KUBECONFIG="$OPTARG"
        shift 2
        clean-hub-cluster "$@"
        ;;
        s)
        echo "Cleaning up DR Resources in spoke cluster"
        export KUBECONFIG="$OPTARG"
        shift 2
        clean-spoke-cluster "$@"
        ;;
        *)
        echo "Invalid option"
    esac
done