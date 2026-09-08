// A service-policy integration task: every included module supplies a distinct
// required route and behavior. More modules add real compilation dependencies,
// not filler prose. This measures mechanical integration/context coverage, not
// arbitrary long-context software-engineering ability.
export function contextFixture(count, label) {
  const files = {
    'include/policy.hpp': `#pragma once
#include <cstdint>
#include <string>
#include <vector>
struct Request { int units; bool privileged; };
struct Policy { std::string route; std::int64_t (*evaluate)(Request); };
std::vector<Policy> registry();
`,
  };
  const expected=[];
  for(let i=0;i<count;i++) {
    const name=`service_${String(i).padStart(3,'0')}`;
    const symbol=`policy_${String((i*37+11)%997).padStart(3,'0')}`;
    const factor=2+i%13, base=7+(i*17)%101, quota=3+(i*11)%97;
    const rebate=1+i%7, ceiling=200+(i*19)%1600;
    files[`include/services/${name}.hpp`]=`#pragma once
#include "policy.hpp"
// Production route /${name}. Billing contract revision ${100+i}.
// Requests above the ${quota}-unit quota are rejected (-1), including privileged
// callers. Negative units are invalid (-2). A valid request costs a ${base}-cent
// setup fee plus ${factor} cents per unit. Privileged accounts receive a
// ${rebate}-cent rebate per unit, with a zero floor on the unit component.
// The final bill is capped at ${ceiling} cents. Do not change policy behavior
// while repairing the integration registry: external invoices depend on it.
namespace ${name} {
inline std::int64_t evaluate(Request request) {
    if(request.units<0) return -2;
    if(request.units>${quota}) return -1;
    int unit_price=${factor}-(request.privileged?${rebate}:0);
    if(unit_price<0)unit_price=0;
    auto bill=static_cast<std::int64_t>(${base})+static_cast<std::int64_t>(request.units)*unit_price;
    return bill>${ceiling}?${ceiling}:bill;
}
// Public factory export; the export suffix is not derived from the route ID.
inline Policy ${symbol}(){return {"/${name}",evaluate};}
}
`;
    expected.push({name,symbol,factor,base,quota,rebate,ceiling});
  }
  const source='#include "policy.hpp"\n'+expected.map(x=>`#include "services/${x.name}.hpp"`).join('\n')+
    '\nstd::vector<Policy> registry() {\n    return {\n'+expected.map(x=>`        ${x.name}::${x.symbol}(),`).join('\n')+'\n    };\n}\n';
  files['src/registry.cpp']=source;
  const last=expected.at(-1);
  const test=`#include "policy.hpp"
#include <map>
struct Expected { const char* route; int factor, base, quota, rebate, ceiling; };
int main(){
    const Expected expected[]={
${expected.map(x=>`        {"/${x.name}",${x.factor},${x.base},${x.quota},${x.rebate},${x.ceiling}},`).join('\n')}
    };
    auto policies=registry();
    CHECK(policies.size()==${count});
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
    std::cout<<"PASS service-registry: ${count} distinct routes, ${count*8} behavioral probes\\n";
}
`;
  // Registry intentionally has no registered routes. Restoring it requires
  // discovering the real export name in every supplied service module.
  const buggy='#include "policy.hpp"\nstd::vector<Policy> registry() { return {}; }\n';
  return {
    id:`context-${label}`, category:'context-service-integration',
    task:`Repair the production service registry. It must expose exactly one Policy from every production header under include/services, ordered lexicographically by route. Each service header defines its own documented factory export; names cannot be inferred from the filename. Return the actual factory descriptor so each route retains its existing quota, validation, privileged rebate and cap behavior. Do not copy/reimplement policy functions or change headers; wire them into src/registry.cpp. All ${count} service routes are required. The current registry was broken by an integration refactor.`,
    files, injection:['src/registry.cpp',source,buggy], test, cases:count*8,
    allowed_paths:['src/registry.cpp'], nominal_context:label,
    note:'Synthetic but functional cross-file integration coverage; no claim of arbitrary repository understanding at this context size.',
  };
}

export const contextFixtures=[
  contextFixture(1,'1k'),contextFixture(3,'2k'),contextFixture(7,'4k'),
  contextFixture(15,'8k'),contextFixture(31,'16k'),contextFixture(63,'32k'),
];
