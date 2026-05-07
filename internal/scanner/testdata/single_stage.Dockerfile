FROM python:3.12-slim
WORKDIR /srv
ENV LOG_LEVEL info
RUN pip install fastapi
CMD ["python", "-m", "app"]
