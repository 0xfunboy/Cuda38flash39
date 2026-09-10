#pragma once
#include "policy.hpp"
// Production route /service_000. Billing contract revision 100.
// Requests above the 3-unit quota are rejected (-1), including privileged
// callers. Negative units are invalid (-2). A valid request costs a 7-cent
// setup fee plus 2 cents per unit. Privileged accounts receive a
// 1-cent rebate per unit, with a zero floor on the unit component.
// The final bill is capped at 200 cents. Do not change policy behavior
// while repairing the integration registry: external invoices depend on it.
namespace service_000 {
inline std::int64_t evaluate(Request request) {
    if(request.units<0) return -2;
    if(request.units>3) return -1;
    int unit_price=2-(request.privileged?1:0);
    if(unit_price<0)unit_price=0;
    auto bill=static_cast<std::int64_t>(7)+static_cast<std::int64_t>(request.units)*unit_price;
    return bill>200?200:bill;
}
// Public factory export; the export suffix is not derived from the route ID.
inline Policy policy_011(){return {"/service_000",evaluate};}
}
