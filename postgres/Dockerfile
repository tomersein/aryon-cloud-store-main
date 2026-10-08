FROM postgres:15

# Set environment variables
ENV POSTGRES_USER=aryon
ENV POSTGRES_PASSWORD=aryon
ENV POSTGRES_DB=aryondb

RUN mkdir -p /docker-entrypoint-initdb.d

# Copy initialization scripts
COPY ./init.sql /docker-entrypoint-initdb.d/

# Expose the PostgreSQL port
EXPOSE 5432

# Set the default command
CMD ["postgres"]