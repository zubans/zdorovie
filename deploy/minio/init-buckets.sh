#!/bin/sh
set -e

MC_BIN="$(command -v mc || echo /usr/bin/mc)"

echo "Waiting for MinIO service to start..."
until "$MC_BIN" alias set local "http://${MINIO_ENDPOINT:-minio:9000}" "${MINIO_ROOT_USER:-minio_admin}" "${MINIO_ROOT_PASSWORD:-minio_secret_key}"; do
    echo "MinIO is not ready yet, sleeping 2 seconds..."
    sleep 2
done

echo "MinIO connection established successfully."

# Create buckets if they do not exist
for bucket in "${MINIO_BUCKET_MASTER:-zdorovie-master}" "${MINIO_BUCKET_CLIENT:-zdorovie-client-materials}" "${MINIO_BUCKET_CONSULTATIONS:-zdorovie-consultations}"; do
    if "$MC_BIN" ls "local/$bucket" > /dev/null 2>&1; then
        echo "Bucket $bucket already exists."
    else
        echo "Creating bucket: $bucket"
        "$MC_BIN" mb "local/$bucket"
    fi
    echo "Setting private access policy for $bucket"
    "$MC_BIN" anonymous set none "local/$bucket" || true
done

echo "MinIO buckets initialized successfully:"
"$MC_BIN" ls local/
exit 0
