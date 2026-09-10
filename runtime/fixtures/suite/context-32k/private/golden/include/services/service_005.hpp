#pragma once
#include "policy.hpp"
// Production route /service_005. Billing contract revision 105.
// Requests above the 58-unit quota are rejected (-1), including privileged
// callers. Negative units are invalid (-2). A valid request costs a 92-cent
// setup fee plus 7 cents per unit. Privileged accounts receive a
// 6-cent rebate per unit, with a zero floor on the unit component.
// The final bill is capped at 295 cents. Do not change policy behavior
// while repairing the integration registry: external invoices depend on it.
namespace service_005 {
inline std::int64_t evaluate(Request request) {
    if(request.units<0) return -2;
    if(request.units>58) return -1;
    int unit_price=7-(request.privileged?6:0);
    if(unit_price<0)unit_price=0;
    auto bill=static_cast<std::int64_t>(92)+static_cast<std::int64_t>(request.units)*unit_price;
    return bill>295?295:bill;
}
// Public factory export; the export suffix is not derived from the route ID.
inline Policy policy_196(){return {"/service_005",evaluate};}
}
