#include "cache.hpp"
void Cache::purge(std::int64_t now) {
    for (auto it=entries.begin(); it!=entries.end();) {
        if (it->second.expires <= now) { lru.erase(it->second.order); it=entries.erase(it); }
        else ++it;
    }
}
void Cache::put(const std::string& key, std::string value, std::int64_t now, std::int64_t ttl) {
    purge(now);
    auto old=entries.find(key);
    if(old!=entries.end()) { lru.erase(old->second.order); entries.erase(old); }
    if(ttl<=0 || capacity==0) return;
    while(entries.size()>=capacity) { entries.erase(lru.back()); lru.pop_back(); }
    lru.push_front(key);
    entries.emplace(key, Entry{std::move(value),now+ttl,lru.begin()});
}
std::optional<std::string> Cache::get(const std::string& key,std::int64_t now) {
    purge(now);
    auto it=entries.find(key);
    if(it==entries.end()) return std::nullopt;
    lru.splice(lru.begin(),lru,it->second.order);
    return it->second.value;
}
