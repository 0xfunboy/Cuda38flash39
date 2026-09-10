#pragma once
#include "policy.hpp"
// Production route /service_060. Billing contract revision 160.
// Requests above the 81-unit quota are rejected (-1), including privileged
// callers. Negative units are invalid (-2). A valid request costs a 17-cent
// setup fee plus 10 cents per unit. Privileged accounts receive a
// 5-cent rebate per unit, with a zero floor on the unit component.
// The final bill is capped at 1340 cents. Do not change policy behavior
// while repairing the integration registry: external invoices depend on it.
namespace service_060 {
inline std::int64_t evaluate(Request request) {
    if(request.units<0) return -2;
    if(request.units>81) return -1;
    int unit_price=10-(request.privileged?5:0);
    if(unit_price<0)unit_price=0;
    auto bill=static_cast<std::int64_t>(17)+static_cast<std::int64_t>(request.units)*unit_price;
    return bill>1340?1340:bill;
}
// Public factory export; the export suffix is not derived from the route ID.
inline Policy policy_237(){return {"/service_060",evaluate};}
}
