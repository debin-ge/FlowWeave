ARG OPENSEARCH_VERSION=2.19.4
FROM opensearchproject/opensearch:${OPENSEARCH_VERSION}

ARG OPENSEARCH_IK_PLUGIN_URL

RUN test -n "${OPENSEARCH_IK_PLUGIN_URL}" \
    || (echo "ERROR: OPENSEARCH_IK_PLUGIN_URL is required to install Chinese analyzer plugin (IK)." && exit 1)

RUN /usr/share/opensearch/bin/opensearch-plugin install --batch "${OPENSEARCH_IK_PLUGIN_URL}"

RUN /usr/share/opensearch/bin/opensearch-plugin list | grep -E "ik|analysis-ik" >/dev/null
