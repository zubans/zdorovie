#!/bin/sh
set -e

echo "Waiting for MinIO service to start..."
until /usr/bin/mc alias set local "http://${MINIO_ENDPOINT:-minio:9000}" "${MINIO_ROOT_USER:-minio_admin}" "${MINIO_ROOT_PASSWORD:-minio_secret_key}"; do
    echo "MinIO is not ready yet, sleeping 2 seconds..."
    sleep 2
done

echo "MinIO connection established successfully."

# Create buckets if they do not exist
for bucket in "${MINIO_BUCKET_MASTER:-zdorovie-master}" "${MINIO_BUCKET_CLIENT:-zdorovie-client-materials}" "${MINIO_BUCKET_CONSULTATIONS:-zdorovie-consultations}"; do
    if /usr/bin/mc ls "local/$bucket" > /dev/null 2>&1; then
        echo "Bucket $bucket already exists."
    else
        echo "Creating bucket: $bucket"
        /usr/bin/mc mb "local/$bucket"
    fi
    echo "Setting private access policy for $bucket"
    /usr/bin/mc anonymous set none "local/$bucket" || true
done

echo "MinIO buckets initialized successfully:"
/usr/bin/mc ls local/
exit 0
