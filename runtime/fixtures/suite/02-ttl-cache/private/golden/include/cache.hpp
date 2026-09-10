#pragma once
#include <cstdint>
#include <list>
#include <optional>
#include <string>
#include <unordered_map>
class Cache {
    struct Entry { std::string value; std::int64_t expires; std::list<std::string>::iterator order; };
    std::size_t capacity;
    std::list<std::string> lru;
    std::unordered_map<std::string,Entry> entries;
    void purge(std::int64_t now);
public:
    explicit Cache(std::size_t limit): capacity(limit) {}
    void put(const std::string& key, std::string value, std::int64_t now, std::int64_t ttl);
    std::optional<std::string> get(const std::string& key, std::int64_t now);
};
