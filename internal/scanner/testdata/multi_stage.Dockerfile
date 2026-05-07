# Sample multi-stage Dockerfile used by dockerfile_test.go.
FROM node:20 AS builder
WORKDIR /app
COPY . .
RUN npm ci && npm run build

FROM python:3.12-slim
WORKDIR /app
COPY --from=builder /app/dist /app
ENV PYTHONUNBUFFERED=1
RUN apt-get update && apt-get install -y libpq-dev gcc && rm -rf /var/lib/apt/lists/*
RUN pip install --no-cache-dir fastapi==0.135.1 sqlalchemy==2.0.48
RUN git clone https://github.com/foo/native-lib /opt/native-lib
ARG BUILD_REV
USER appuser
EXPOSE 8000
CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0"]
