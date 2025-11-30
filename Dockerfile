# Multi-stage build for SMS Mock Server

# Stage 1: Base image with dependencies
FROM python:3.13-alpine AS base

# Set working directory
WORKDIR /app

# Copy requirements and install Python dependencies
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy static files and build script, then build assets
COPY static/ ./static/
COPY scripts/ ./scripts/
RUN python scripts/build_assets.py

# Stage 2: Final image
FROM python:3.13-alpine

# Set working directory
WORKDIR /app

# Copy Python dependencies from base stage
COPY --from=base /usr/local/lib/python3.13/site-packages /usr/local/lib/python3.13/site-packages
COPY --from=base /usr/local/bin /usr/local/bin

# Copy built static assets (includes dist/ and manifest.json)
COPY --from=base /app/static/ ./static/

# Copy application code
COPY app/ ./app/
COPY templates/ ./templates/
COPY config.yaml .

# Create data directory
RUN mkdir -p data

# Expose ports
EXPOSE 8080

# Set environment variables
ENV PYTHONUNBUFFERED=1
ENV LOG_LEVEL=INFO

# Run the application
CMD ["python", "-m", "app.main"]
