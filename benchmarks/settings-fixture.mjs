// Tenant configuration repository with dependency-aware memoization. Both the
// golden and hidden oracle are private preparation assets, never prompt files.
export const settingsFixture={
  id:'08-tenant-settings-cache',category:'multi-file-dependency-cache-atomic-mutation',
  task:'Repair the tenant settings service. Users report that inherited settings sometimes remain stale, and a rejected tenant reparent can affect subsequent operations. Follow the public contract in README.md; preserve all APIs, valid behavior, memoization and cache hits for unrelated branches. Diagnose the repository and produce the smallest complete fix. Do not remove caching, globally flush it on every mutation, change tests or bypass the resolution_steps instrumentation.',
  files:{
    'README.md':`# Tenant settings service

Settings resolve through a single-parent tenant hierarchy. A tenant's local
override wins; otherwise lookup continues at its parent. A nullopt local value
is an explicit tombstone: it masks inherited values. An absent override inherits.
An empty string is a real value, distinct from both absence and a tombstone.

add_tenant(name, parent) requires a nonempty unique name and an existing parent
if provided. Invalid/duplicate names throw invalid_argument; unknown tenant or
parent throws out_of_range. set_value/erase_override require nonempty keys.
Every successful set_value, erase_override and set_parent increments that
tenant's revision exactly once, including same-value writes and absent erases.

set_parent must reject self-parenting and indirect cycles with invalid_argument.
A rejected mutation is atomic: no parent, value, revision or lookup result may
change. Detaching via nullopt is valid. Lookup of an unknown tenant throws
out_of_range; an empty key throws invalid_argument.

Memoized lookups depend on ALL tenants consulted in the resolution path, up to
and including the one providing a value/tombstone, or the root for missing keys.
Any successful mutation of a consulted tenant invalidates that result. Mutating
an unrelated branch must preserve cache hits. Positive and negative lookups are
cached; repeated lookups without dependent changes perform zero additional
resolution steps. resolution_steps counts visited tenants only during a fresh
resolution; it does not count dependency validation on an existing cache entry.
The counter exists for service diagnostics and must remain truthful.

The library is single-threaded. No tenant deletion or concurrency is in scope.
`,
    'include/settings.hpp':String.raw`#pragma once
#include <cstdint>
#include <map>
#include <optional>
#include <string>
#include <utility>
#include <vector>
class Settings {
    struct Tenant {
        std::optional<std::string> parent;
        std::map<std::string,std::optional<std::string>> values;
        std::uint64_t revision=0;
    };
    struct Memo {
        std::optional<std::string> value;
        std::vector<std::pair<std::string,std::uint64_t>> dependencies;
    };
    std::map<std::string,Tenant> tenants_;
    mutable std::map<std::pair<std::string,std::string>,Memo> cache_;
    mutable std::uint64_t steps_=0;
public:
    void add_tenant(const std::string& name,std::optional<std::string> parent=std::nullopt);
    void set_parent(const std::string& name,std::optional<std::string> parent);
    void set_value(const std::string& tenant,const std::string& key,std::optional<std::string> value);
    void erase_override(const std::string& tenant,const std::string& key);
    std::optional<std::string> get(const std::string& tenant,const std::string& key) const;
    std::uint64_t revision(const std::string& tenant) const;
    std::uint64_t resolution_steps() const noexcept {return steps_;}
};
`,
    'src/mutations.cpp':String.raw`#include "settings.hpp"
#include <stdexcept>
void Settings::add_tenant(const std::string& name,std::optional<std::string> parent){
    if(name.empty()||tenants_.count(name))throw std::invalid_argument("tenant name");
    if(parent&&!tenants_.count(*parent))throw std::out_of_range("parent");
    tenants_.emplace(name,Tenant{std::move(parent),{},0});
}
void Settings::set_parent(const std::string& name,std::optional<std::string> parent){
    auto& tenant=tenants_.at(name);
    if(parent&&!tenants_.count(*parent))throw std::out_of_range("parent");
    auto ancestor=parent;
    while(ancestor){
        if(*ancestor==name)throw std::invalid_argument("parent cycle");
        ancestor=tenants_.at(*ancestor).parent;
    }
    tenant.parent=std::move(parent);
    ++tenant.revision;
}
void Settings::set_value(const std::string& name,const std::string& key,std::optional<std::string> value){
    if(key.empty())throw std::invalid_argument("key");
    auto& tenant=tenants_.at(name);
    tenant.values[key]=std::move(value);
    ++tenant.revision;
}
void Settings::erase_override(const std::string& name,const std::string& key){
    if(key.empty())throw std::invalid_argument("key");
    auto& tenant=tenants_.at(name);
    tenant.values.erase(key);
    ++tenant.revision;
}
std::uint64_t Settings::revision(const std::string& name) const {return tenants_.at(name).revision;}
`,
    'src/resolver.cpp':String.raw`#include "settings.hpp"
#include <set>
#include <stdexcept>
std::optional<std::string> Settings::get(const std::string& name,const std::string& key) const {
    if(key.empty())throw std::invalid_argument("key");
    tenants_.at(name);
    const auto cache_key=std::make_pair(name,key);
    auto memo=cache_.find(cache_key);
    if(memo!=cache_.end()){
        bool valid=true;
        for(const auto& [tenant,revision]:memo->second.dependencies)
            if(tenants_.at(tenant).revision!=revision){valid=false;break;}
        if(valid)return memo->second.value;
    }
    Memo next;
    std::optional<std::string> current=name;
    std::set<std::string> seen;
    while(current){
        if(!seen.insert(*current).second)throw std::logic_error("corrupt hierarchy");
        const auto& tenant=tenants_.at(*current);
        ++steps_;
        next.dependencies.emplace_back(*current,tenant.revision);
        const auto value=tenant.values.find(key);
        if(value!=tenant.values.end()){next.value=value->second;break;}
        current=tenant.parent;
    }
    cache_[cache_key]=next;
    return next.value;
}
`,
  },
  injections:[
    ['src/resolver.cpp','next.dependencies.emplace_back(*current,tenant.revision);','if(tenant.values.count(key))next.dependencies.emplace_back(*current,tenant.revision);'],
    ['src/mutations.cpp',String.raw`    auto ancestor=parent;
    while(ancestor){
        if(*ancestor==name)throw std::invalid_argument("parent cycle");
        ancestor=tenants_.at(*ancestor).parent;
    }
    tenant.parent=std::move(parent);
    ++tenant.revision;`,String.raw`    tenant.parent=parent;
    ++tenant.revision;
    auto ancestor=parent;
    while(ancestor){
        if(*ancestor==name)throw std::invalid_argument("parent cycle");
        ancestor=tenants_.at(*ancestor).parent;
    }`],
  ],
  allowed_paths:['src/resolver.cpp','src/mutations.cpp'],
  test:String.raw`#include "settings.hpp"
template<class F>bool missing(F fn){try{fn();}catch(const std::out_of_range&){return true;}return false;}
int main(){
    Settings s;s.add_tenant("root");s.add_tenant("mid","root");s.add_tenant("leaf","mid");s.add_tenant("other");
    s.set_value("root","color","blue");
    CHECK(s.get("leaf","color")=="blue");auto hit=s.resolution_steps();CHECK(s.get("leaf","color")=="blue");CHECK(s.resolution_steps()==hit);
    s.set_value("mid","color","red");CHECK(s.get("leaf","color")=="red");
    s.set_value("leaf","color",std::nullopt);CHECK(!s.get("leaf","color"));
    s.erase_override("leaf","color");CHECK(s.get("leaf","color")=="red");
    s.set_value("mid","color",std::string{});CHECK(s.get("leaf","color")==std::string{});
    s.erase_override("mid","color");CHECK(s.get("leaf","color")=="blue");
    CHECK(!s.get("leaf","missing"));auto negative=s.resolution_steps();CHECK(!s.get("leaf","missing"));CHECK(s.resolution_steps()==negative);
    s.set_value("root","missing","found");CHECK(s.get("leaf","missing")=="found");
    auto independent=s.resolution_steps();s.set_value("other","noise","x");CHECK(s.get("leaf","missing")=="found");CHECK(s.resolution_steps()==independent);
    s.set_value("other","color","green");s.set_parent("mid","other");CHECK(s.get("leaf","color")=="green");
    s.set_parent("mid",std::nullopt);CHECK(!s.get("leaf","color"));s.set_parent("mid","root");CHECK(s.get("leaf","color")=="blue");
    auto root_revision=s.revision("root"),mid_revision=s.revision("mid"),leaf_revision=s.revision("leaf");
    CHECK(invalid([&]{s.set_parent("root","leaf");}));CHECK(s.revision("root")==root_revision);CHECK(s.revision("mid")==mid_revision);CHECK(s.revision("leaf")==leaf_revision);CHECK(s.get("leaf","color")=="blue");
    CHECK(invalid([&]{s.set_parent("mid","mid");}));CHECK(s.revision("mid")==mid_revision);CHECK(s.get("leaf","color")=="blue");
    CHECK(missing([&]{s.set_parent("mid","absent");}));CHECK(s.revision("mid")==mid_revision);
    CHECK(missing([&]{s.get("absent","color");}));CHECK(invalid([&]{s.get("root","");}));
    CHECK(invalid([&]{s.set_value("root","","x");}));CHECK(s.revision("root")==root_revision);
    auto before=s.revision("root");s.set_value("root","color","blue");CHECK(s.revision("root")==before+1);s.erase_override("root","never-set");CHECK(s.revision("root")==before+2);
    // Deterministic alternating mutations exercise stale negative and positive
    // paths while an unrelated memo stays hot. Expected values are direct state.
    Settings chain;chain.add_tenant("a");chain.add_tenant("b","a");chain.add_tenant("c","b");chain.add_tenant("z");chain.set_value("z","stable","yes");
    for(int i=0;i<80;++i){
        chain.set_value("a","k",std::to_string(i));chain.erase_override("b","k");CHECK(chain.get("c","k")==std::to_string(i));
        chain.set_value("b","k",std::nullopt);CHECK(!chain.get("c","k"));
        CHECK(chain.get("z","stable")=="yes");auto steps=chain.resolution_steps();chain.set_value("a","k","again");CHECK(chain.get("z","stable")=="yes");CHECK(chain.resolution_steps()==steps);
    }
    std::cout<<"PASS tenant-settings-cache: hierarchy/tombstone/negative-cache/atomic-reparent and 80 alternating mutation sequences\n";
}
`,cases:80,
};
