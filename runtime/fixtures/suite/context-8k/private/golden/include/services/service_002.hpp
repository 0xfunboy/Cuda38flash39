#pragma once
#include "policy.hpp"
// Production route /service_002. Billing contract revision 102.
// Requests above the 25-unit quota are rejected (-1), including privileged
// callers. Negative units are invalid (-2). A valid request costs a 41-cent
// setup fee plus 4 cents per unit. Privileged accounts receive a
// 3-cent rebate per unit, with a zero floor on the unit component.
// The final bill is capped at 238 cents. Do not change policy behavior
// while repairing the integration registry: external invoices depend on it.
namespace service_002 {
inline std::int64_t evaluate(Request request) {
    if(request.units<0) return -2;
    if(request.units>25) return -1;
    int unit_price=4-(request.privileged?3:0);
    if(unit_price<0)unit_price=0;
    auto bill=static_cast<std::int64_t>(41)+static_cast<std::int64_t>(request.units)*unit_price;
    return bill>238?238:bill;
}
// Public factory export; the export suffix is not derived from the route ID.
inline Policy policy_085(){return {"/service_002",evaluate};}
}
