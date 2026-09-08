// One final progressive asynchronous-service fixture. The executor is explicit
// and deterministic: no threads, sleeps, network, timing assumptions or GLM calls.
export const coalescingFixture={
  id:'09-request-coalescing-cache',category:'async-coalescing-generation-reentrancy',
  task:'Repair the request-coalescing cache service. Users report lost callbacks when completion handlers issue another request, and inconsistent values when invalidation overlaps outstanding loads. Preserve the public API and the README.md behavioral contract. Read the relevant source files, diagnose the interactions, and deliver a correct small patch. Do not remove coalescing/caching or disable callbacks/checks. No threads or new dependencies are required.',
  files:{
    'README.md':`# Request-coalescing cache service

The service is used on a single event-loop thread. request(key, callback) returns
via the callback with an Outcome. A successful cached value is delivered inline.
Concurrent outstanding requests for the same key and generation share one loader
invocation, and every registered waiter receives exactly one completion.

The supplied Loader can complete inline or later via its Done callback. It can
also throw std::exception; this becomes a failure carrying exception.what().
Failures are not cached, so a following request starts a new load. Duplicate Done
calls for the same flight are ignored, including a throw after inline completion.
Consumer callbacks are required not to throw; they may reenter request/invalidate.
The AsyncCache object outlives all outstanding loader callbacks.

invalidate(key) removes its successful cache entry and starts a fresh generation.
Existing waiters still receive their own old-generation completion exactly once.
New requests must not join an invalidated old flight. Old completions must not
overwrite current cache entries or remove/complete a newer flight. Independent
keys must not interfere. Empty values are successful cacheable values.

Completion state must be committed before invoking consumer callbacks: callback
code may invalidate the key, issue another request, or observe cache/in-flight
counts. Successful completion makes its value visible; failed completion allows
immediate retry. A reentrant callback's mutation must not be undone afterwards.

Empty keys and missing callbacks throw invalid_argument before changing state.
in_flight_count counts current-generation loads only; invalidated loads can still
be outstanding externally. cached_count counts successful current cache entries.
The service has no timeout, eviction, cancellation, persistence or threading API.
`,
    'include/async_cache.hpp':String.raw`#pragma once
#include <cstdint>
#include <functional>
#include <map>
#include <memory>
#include <string>
#include <utility>
#include <vector>
struct Outcome {
    bool ok;
    std::string value,error;
    static Outcome success(std::string value){return {true,std::move(value),{}};}
    static Outcome failure(std::string error){return {false,{},std::move(error)};}
};
class AsyncCache {
public:
    using Callback=std::function<void(Outcome)>;
    using Loader=std::function<void(const std::string&,Callback)>;
    explicit AsyncCache(Loader loader);
    void request(const std::string& key,Callback callback);
    void invalidate(const std::string& key);
    std::size_t in_flight_count()const noexcept{return current_.size();}
    std::size_t cached_count()const noexcept{return values_.size();}
private:
    struct Flight {
        std::string key;
        std::uint64_t generation;
        std::vector<Callback> waiters;
        bool completed=false;
    };
    Loader loader_;
    std::map<std::string,std::string> values_;
    std::map<std::string,std::uint64_t> generations_;
    std::map<std::string,std::shared_ptr<Flight>> current_;
    void finish(const std::shared_ptr<Flight>& flight,Outcome result);
};
`,
    'src/requests.cpp':String.raw`#include "async_cache.hpp"
#include <stdexcept>
AsyncCache::AsyncCache(Loader loader):loader_(std::move(loader)){
    if(!loader_)throw std::invalid_argument("loader");
}
void AsyncCache::request(const std::string& key,Callback callback){
    if(key.empty()||!callback)throw std::invalid_argument("request");
    auto cached=values_.find(key);
    if(cached!=values_.end()){callback(Outcome::success(cached->second));return;}
    auto running=current_.find(key);
    if(running!=current_.end()){running->second->waiters.push_back(std::move(callback));return;}
    auto flight=std::make_shared<Flight>();flight->key=key;flight->generation=generations_[key];
    flight->waiters.push_back(std::move(callback));current_[key]=flight;
    try{loader_(key,[this,flight](Outcome result){finish(flight,std::move(result));});}
    catch(const std::exception& error){finish(flight,Outcome::failure(error.what()));}
}
void AsyncCache::invalidate(const std::string& key){
    if(key.empty())throw std::invalid_argument("key");
    ++generations_[key];values_.erase(key);current_.erase(key);
}
`,
    'src/completion.cpp':String.raw`#include "async_cache.hpp"
void AsyncCache::finish(const std::shared_ptr<Flight>& flight,Outcome result){
    if(flight->completed)return;
    flight->completed=true;
    auto current=current_.find(flight->key);
    const bool owns_current=current!=current_.end()&&current->second==flight&&generations_[flight->key]==flight->generation;
    if(owns_current){
        current_.erase(flight->key);
        if(result.ok)values_[flight->key]=result.value;
        else values_.erase(flight->key);
    }
    auto waiters=std::move(flight->waiters);
    for(auto& callback:waiters)callback(result);
}
`,
  },
  injections:[
    ['src/completion.cpp','current!=current_.end()&&current->second==flight&&generations_[flight->key]==flight->generation','current!=current_.end()'],
    ['src/completion.cpp',String.raw`    if(owns_current){
        current_.erase(flight->key);
        if(result.ok)values_[flight->key]=result.value;
        else values_.erase(flight->key);
    }
    auto waiters=std::move(flight->waiters);
    for(auto& callback:waiters)callback(result);`,String.raw`    auto waiters=std::move(flight->waiters);
    for(auto& callback:waiters)callback(result);
    if(owns_current){
        current_.erase(flight->key);
        if(result.ok)values_[flight->key]=result.value;
        else values_.erase(flight->key);
    }`],
  ],
  allowed_paths:['src/requests.cpp','src/completion.cpp'],
  test:String.raw`#include "async_cache.hpp"
#include <deque>

struct ManualLoader {
    struct Pending{std::string key;AsyncCache::Callback done;};
    std::vector<Pending> pending;
    AsyncCache::Loader loader(){return [this](const std::string& key,AsyncCache::Callback done){pending.push_back({key,std::move(done)});};}
    void complete(std::size_t index,Outcome result){auto fn=pending.at(index).done;fn(std::move(result));}
};
struct Seen{int calls=0;std::vector<Outcome> results;AsyncCache::Callback callback(){return [this](Outcome result){++calls;results.push_back(std::move(result));};}};
int main(){
    // Coalescing plus reentrancy: a cached hit requested from completion must
    // complete inline without starting or joining another load.
    ManualLoader loader;AsyncCache cache(loader.loader());Seen first,second,nested;
    cache.request("key",[&](Outcome result){first.callback()(result);cache.request("key",nested.callback());});
    cache.request("key",second.callback());CHECK(loader.pending.size()==1);CHECK(cache.in_flight_count()==1);
    loader.complete(0,Outcome::success("value"));CHECK(first.calls==1&&second.calls==1&&nested.calls==1);
    CHECK(nested.results[0].ok&&nested.results[0].value=="value");CHECK(cache.in_flight_count()==0&&cache.cached_count()==1);CHECK(loader.pending.size()==1);
    loader.complete(0,Outcome::failure("duplicate"));CHECK(first.calls==1&&second.calls==1&&nested.calls==1);
    Seen hit;cache.request("key",hit.callback());CHECK(hit.calls==1&&hit.results[0].value=="value");CHECK(loader.pending.size()==1);

    // The old generation completes both before and after the newer flight.
    for(bool old_first:{false,true}){
        ManualLoader m;AsyncCache c(m.loader());Seen old,newer,join,final;
        c.request("a",old.callback());c.invalidate("a");c.request("a",newer.callback());CHECK(m.pending.size()==2);
        if(old_first){m.complete(0,Outcome::success("old"));CHECK(old.calls==1&&newer.calls==0);CHECK(c.cached_count()==0&&c.in_flight_count()==1);c.request("a",join.callback());CHECK(m.pending.size()==2);m.complete(1,Outcome::success("new"));CHECK(join.calls==1&&join.results[0].value=="new");}
        else{m.complete(1,Outcome::success("new"));m.complete(0,Outcome::success("old"));}
        CHECK(old.calls==1&&old.results[0].value=="old");CHECK(newer.calls==1&&newer.results[0].value=="new");
        c.request("a",final.callback());CHECK(final.calls==1&&final.results[0].value=="new");CHECK(m.pending.size()==2);
    }

    // Invalidation and new work started INSIDE a consumer callback survive the
    // outer completion; no later cleanup may erase the new flight or cache.
    {ManualLoader m;AsyncCache c(m.loader());Seen renewed,joined;
      c.request("a",[&](Outcome){c.invalidate("a");c.request("a",renewed.callback());});
      m.complete(0,Outcome::success("old"));CHECK(m.pending.size()==2);CHECK(c.in_flight_count()==1&&c.cached_count()==0);
      c.request("a",joined.callback());CHECK(m.pending.size()==2);m.complete(1,Outcome::success("new"));CHECK(renewed.calls==1&&joined.calls==1);CHECK(renewed.results[0].value=="new"&&joined.results[0].value=="new");}

    // Reentrant retry after error, plus stale failure after a newer success.
    {ManualLoader m;AsyncCache c(m.loader());Seen failed,retry;
      c.request("a",[&](Outcome r){failed.callback()(r);if(!r.ok)c.request("a",retry.callback());});
      m.complete(0,Outcome::failure("temporary"));CHECK(failed.calls==1&&!failed.results[0].ok&&failed.results[0].error=="temporary");CHECK(m.pending.size()==2&&c.cached_count()==0);
      m.complete(1,Outcome::success("recovered"));CHECK(retry.calls==1&&retry.results[0].value=="recovered");}
    {ManualLoader m;AsyncCache c(m.loader());Seen old,current,hit2;
      c.request("x",old.callback());c.invalidate("x");c.request("x",current.callback());m.complete(1,Outcome::success("new"));m.complete(0,Outcome::failure("old failure"));
      c.request("x",hit2.callback());CHECK(old.calls==1&&!old.results[0].ok);CHECK(hit2.calls==1&&hit2.results[0].value=="new");CHECK(m.pending.size()==2);}

    // Independent keys, empty values and invalid requests preserve state.
    {ManualLoader m;AsyncCache c(m.loader());Seen a,b,empty;
      c.request("a",a.callback());c.request("b",b.callback());CHECK(c.in_flight_count()==2);m.complete(1,Outcome::success(""));m.complete(0,Outcome::success("A"));
      c.request("b",empty.callback());CHECK(a.calls==1&&b.calls==1&&empty.calls==1&&empty.results[0].ok&&empty.results[0].value.empty());
      CHECK(invalid([&]{c.request("",a.callback());}));CHECK(invalid([&]{c.request("x",{});}));CHECK(invalid([&]{c.invalidate("");}));CHECK(c.cached_count()==2&&c.in_flight_count()==0&&m.pending.size()==2);}

    // Real loader exceptions are results; inline callback and duplicate/throw
    // completion cannot notify waiters more than once.
    {int calls=0;AsyncCache c([&](const std::string&,AsyncCache::Callback){++calls;throw std::runtime_error("offline");});Seen a,b;c.request("a",a.callback());c.request("a",b.callback());CHECK(calls==2&&a.calls==1&&b.calls==1);CHECK(!a.results[0].ok&&a.results[0].error=="offline");CHECK(c.in_flight_count()==0&&c.cached_count()==0);}
    {int loads=0;AsyncCache c([&](const std::string&,AsyncCache::Callback done){++loads;done(Outcome::success("inline"));done(Outcome::failure("duplicate"));throw std::runtime_error("late");});Seen seen;c.request("a",seen.callback());CHECK(seen.calls==1&&seen.results[0].ok&&seen.results[0].value=="inline");CHECK(loads==1&&c.cached_count()==1&&c.in_flight_count()==0);}
    std::cout<<"PASS request-coalescing-cache: deterministic generations, stale completion, callback ordering, exactly-once, retries and reentrancy\n";
}
`,cases:9,
};
