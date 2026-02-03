FROM gradle:8-jdk17 AS build
WORKDIR /app
COPY build.gradle.kts .
COPY src src
RUN gradle installDist --no-daemon

FROM eclipse-temurin:17-jre
WORKDIR /app
COPY --from=build /app/build/install/temporal-worker-kotlin/ .
CMD ["bin/temporal-worker-kotlin"]
