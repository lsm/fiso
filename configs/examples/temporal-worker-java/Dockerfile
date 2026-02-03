FROM gradle:8-jdk17 AS build
WORKDIR /app
COPY build.gradle .
COPY src src
RUN gradle installDist --no-daemon

FROM eclipse-temurin:17-jre
WORKDIR /app
COPY --from=build /app/build/install/temporal-worker-java/ .
CMD ["bin/temporal-worker-java"]
