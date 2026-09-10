#include <cstdlib>
#include <iostream>
#include <stdexcept>
#include <string>
#define CHECK(x) do { if (!(x)) { std::cerr << "FAIL line " << __LINE__ << ": " << #x << '\n'; return 1; } } while (0)
template<class F> bool invalid(F fn) { try { fn(); } catch (const std::invalid_argument&) { return true; } return false; }
#include "policy.hpp"
#include <map>
struct Expected { const char* route; int factor, base, quota, rebate, ceiling; };
int main(){
    const Expected expected[]={
        {"/service_000",2,7,3,1,200},
        {"/service_001",3,24,14,2,219},
        {"/service_002",4,41,25,3,238},
        {"/service_003",5,58,36,4,257},
        {"/service_004",6,75,47,5,276},
        {"/service_005",7,92,58,6,295},
        {"/service_006",8,8,69,7,314},
        {"/service_007",9,25,80,1,333},
        {"/service_008",10,42,91,2,352},
        {"/service_009",11,59,5,3,371},
        {"/service_010",12,76,16,4,390},
        {"/service_011",13,93,27,5,409},
        {"/service_012",14,9,38,6,428},
        {"/service_013",2,26,49,7,447},
        {"/service_014",3,43,60,1,466},
        {"/service_015",4,60,71,2,485},
        {"/service_016",5,77,82,3,504},
        {"/service_017",6,94,93,4,523},
        {"/service_018",7,10,7,5,542},
        {"/service_019",8,27,18,6,561},
        {"/service_020",9,44,29,7,580},
        {"/service_021",10,61,40,1,599},
        {"/service_022",11,78,51,2,618},
        {"/service_023",12,95,62,3,637},
        {"/service_024",13,11,73,4,656},
        {"/service_025",14,28,84,5,675},
        {"/service_026",2,45,95,6,694},
        {"/service_027",3,62,9,7,713},
        {"/service_028",4,79,20,1,732},
        {"/service_029",5,96,31,2,751},
        {"/service_030",6,12,42,3,770},
    };
    auto policies=registry();
    CHECK(policies.size()==31);
    std::map<std::string,Policy> routes;
    std::string previous;
    for(auto p:policies){CHECK(previous.empty()||previous<p.route);CHECK(routes.emplace(p.route,p).second);previous=p.route;}
    for(auto e:expected){
        CHECK(routes.count(e.route)==1);auto fn=routes.at(e.route).evaluate;
        CHECK(fn({-1,false})==-2);CHECK(fn({e.quota+1,true})==-1);
        for(bool privileged:{false,true})for(int units:{0,1,e.quota}){
            int unit=e.factor-(privileged?e.rebate:0);if(unit<0)unit=0;
            std::int64_t bill=e.base+static_cast<std::int64_t>(unit)*units;if(bill>e.ceiling)bill=e.ceiling;
            CHECK(fn({units,privileged})==bill);
        }
    }
    std::cout<<"PASS service-registry: 31 distinct routes, 248 behavioral probes\n";
}
